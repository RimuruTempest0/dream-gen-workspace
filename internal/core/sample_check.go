package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ---- checking the detail sample before the set copies it (0.7.8) ---------------
//
// Every other image of a set copies the sample, so a wrong sample spreads to all
// of them (11/06: a mini stocking headpiece drawn as a hat). When the sample is
// saved, the LLM compares it with its design references and requirements; a wrong
// one is redrawn with the problems named, at most twice. Images drawn from the
// sample wait until it passed or the redraws are used up.

const (
	maxSampleRedraws   = 2
	sampleCheckTimeout = 3 * time.Minute
	// sampleFixMarker starts the corrections added to a redrawn sample's prompt.
	sampleFixMarker = "\n\n上一版样板核对没通过，这次必须改正："
	// sampleQuality is used for the sample only: the set copies its details.
	sampleQuality = "high"
)

const sampleCheckInstruction = `你是刺绣商品图的质检员。第 1 张图是刚生成的“细节样板”，本套其他图片都会照它画；后面的图是它的设计依据。请按下面的制作要求，逐项检查第 1 张图的设计本身：
- 图案和配饰的形状（例如头饰是哪一种、是什么形状），不能换成相近的其他物品；
- 宠物或人物的品种、毛色、正面或侧面；
- 画面文字的拼写、大小写、字形风格和颜色；
- 各元素的左右顺序、相对位置和相对大小。
只检查以上设计内容，不评价光线、背景、构图或拍摄角度；看不清的地方不算错。
只输出一个 JSON：{"pass": true 或 false, "problems": ["每条一句中文，写清哪里不对、应该是什么样"]}。通过时 problems 为空数组。

制作要求：
`

// startSampleCheckLocked checks a saved sample in the background; it also resumes
// a check left unfinished when the app last exited.
func (s *Service) startSampleCheckLocked(id string) {
	if s.sampleChecks[id] || s.ctx.Err() != nil {
		return
	}
	s.sampleChecks[id] = true
	s.workers.Add(1)
	go func() {
		defer s.workers.Done()
		s.checkSample(id)
		s.mu.Lock()
		delete(s.sampleChecks, id)
		s.mu.Unlock()
		s.signal()
	}()
}

// sampleCheckable reports whether a newly saved task is a sample to check, and
// when it is, marks it so images drawn from it wait.
func (s *Service) markSampleForCheckLocked(t *Task) bool {
	if !t.Sample || t.AgentRunID == "" || t.SampleCheck != "" || t.RefinedFrom != "" || len(t.AssetIDs) == 0 {
		return false
	}
	if _, _, ok := s.rewordLLMLocked(*t); !ok {
		return false
	}
	t.SampleCheck = "checking"
	return true
}

func (s *Service) checkSample(id string) {
	s.mu.Lock()
	var t Task
	if s.store.Get("task", id, &t) != nil || t.Status != "succeeded" || t.SampleCheck != "checking" {
		s.mu.Unlock()
		return
	}
	c, key, ok := s.rewordLLMLocked(t)
	images := append([]string{t.AssetIDs[0]}, t.References...)
	files := make([][]byte, 0, len(images))
	for _, assetID := range images {
		var a Asset
		if s.store.Get("asset", assetID, &a) != nil {
			continue
		}
		if data, e := readOwned(s.store.Root, a.Path); e == nil {
			files = append(files, data)
		}
	}
	s.mu.Unlock()
	pass, problems, e := false, []string{}, errors.New("没有可用的 LLM")
	if ok && len(files) > 0 {
		ctx, cancel := context.WithTimeout(s.ctx, sampleCheckTimeout)
		pass, problems, e = s.askSampleCheck(ctx, c, key, t, files)
		cancel()
	}
	if s.ctx.Err() != nil {
		return // the app is closing; the check resumes on the next start
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.store.Get("task", id, &t) != nil || t.Status != "succeeded" || t.SampleCheck != "checking" {
		return
	}
	t.UpdatedAt = Now()
	switch {
	case e != nil:
		t.SampleCheck, t.SampleProblems = "skipped", "核对没有完成："+cutProductText(redact(e.Error(), key), 300)
		s.addActivityLocked(t.AgentRunID, "warn", "样板图没能核对（"+t.SampleProblems+"），其他图片照它继续生成")
	case pass:
		t.SampleCheck, t.SampleProblems = "passed", ""
		s.addActivityLocked(t.AgentRunID, "tool", "样板图已核对，与设计依据一致")
	default:
		t.SampleProblems = cutProductText(strings.Join(problems, "；"), 1500)
		if t.SampleRedraws < maxSampleRedraws {
			if note := s.redrawSampleLocked(&t); note != "" {
				t.SampleCheck = "failed"
				s.addActivityLocked(t.AgentRunID, "warn", "样板图核对没通过（"+t.SampleProblems+"），"+note)
				break
			}
			t.SampleCheck = "redrawn"
			s.addActivityLocked(t.AgentRunID, "warn", fmt.Sprintf("样板图核对没通过（%s），自动重画（第 %d 次）", t.SampleProblems, t.SampleRedraws+1))
		} else {
			t.SampleCheck = "failed"
			s.addActivityLocked(t.AgentRunID, "warn", fmt.Sprintf("样板图重画 %d 次后仍没通过核对（%s），其他图片照这张继续生成，请留意", t.SampleRedraws, t.SampleProblems))
		}
	}
	_ = s.store.Put("task", t.ID, t)
	_ = s.store.appendLog("info", "agent", t.AgentRunID, fmt.Sprintf("样板图核对：%s %s", t.SampleCheck, t.SampleProblems))
}

// redrawSampleLocked queues a new attempt of the sample with the problems to
// correct. It returns a note when the redraw could not be queued.
func (s *Service) redrawSampleLocked(t *Task) string {
	var live Connection
	if s.store.Get("connection", t.Connection.ID, &live) != nil {
		return "原生图中转站已删除，没有重画"
	}
	next, e := s.queueAttemptLocked(*t, normalizeConnection(live), "")
	if e != nil {
		return "重画没能排队：" + e.Error()
	}
	base, _, _ := strings.Cut(t.Prompt, sampleFixMarker)
	next.Prompt = base + sampleFixMarker + t.SampleProblems
	next.SampleRedraws, next.SampleProblems = t.SampleRedraws+1, t.SampleProblems
	if e = s.store.Put("task", next.ID, next); e != nil {
		return "重画没能排队：" + e.Error()
	}
	return ""
}

func (s *Service) askSampleCheck(ctx context.Context, c Connection, key string, t Task, files [][]byte) (bool, []string, error) {
	content := []any{map[string]any{"type": "text", "text": sampleCheckInstruction + t.Prompt}}
	for _, data := range files {
		block, e := visionImageBlock(data)
		if e != nil {
			return false, nil, e
		}
		content = append(content, block)
	}
	var result struct {
		Pass     bool     `json:"pass"`
		Problems []string `json:"problems"`
	}
	_, e := s.llmAsk(ctx, c, key, "check", "核对样板图", content, func(text string) error {
		start, end := strings.Index(text, "{"), strings.LastIndex(text, "}")
		if start < 0 || end < start {
			return errors.New("核对结果不是 JSON")
		}
		if e := json.Unmarshal([]byte(text[start:end+1]), &result); e != nil {
			return errors.New("核对结果不是 JSON")
		}
		if !result.Pass && len(result.Problems) == 0 {
			return errors.New("核对没通过但没有写出问题")
		}
		return nil
	})
	return result.Pass, result.Problems, e
}

// addActivityLocked adds a line to a run's feed while s.mu is held.
func (s *Service) addActivityLocked(runID, kind, text string) {
	var r AgentRun
	if text = cutActivityText(text); text == "" || s.store.Get("agent_run", runID, &r) != nil {
		return
	}
	r.Activity = append(r.Activity, ActivityEntry{At: Now(), Kind: kind, Text: text})
	if len(r.Activity) > maxActivityEntries {
		r.Activity = r.Activity[len(r.Activity)-maxActivityEntries:]
	}
	_ = s.store.Put("agent_run", r.ID, r)
}
