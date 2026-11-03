package core

import (
	"context"
	"errors"
	"sort"
	"strings"
	"unicode/utf8"
)

type RefineInput struct {
	RequestID   string `json:"requestID"`
	TaskID      string `json:"taskID"`
	AssetID     string `json:"assetID"`
	Instruction string `json:"instruction"`
}

// RefineAgentImage queues one image-edit request that adjusts an existing smart
// generation result. It is a new, possibly billable request: the caller confirms
// it, the request ID makes repeated clicks idempotent, and the original image and
// task are kept. The new task joins the same slot, so it becomes the current version.
func (s *Service) RefineAgentImage(in RefineInput) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	in.Instruction = strings.TrimSpace(in.Instruction)
	if in.RequestID == "" || len(in.RequestID) > 128 {
		return "", errors.New("请求标识无效")
	}
	if in.Instruction == "" {
		return "", errors.New("请填写想怎么调整这张图片")
	}
	if utf8.RuneCountInString(in.Instruction) > 1000 {
		return "", errors.New("调整说明最多1000个字")
	}
	// A repeated click (same request ID) returns the earlier submission unchanged.
	var submitted string
	if s.store.db.QueryRow(`SELECT batch FROM submissions WHERE id=?`, in.RequestID).Scan(&submitted) == nil {
		return submitted, nil
	}
	var source Task
	if e := s.store.Get("task", in.TaskID, &source); e != nil {
		return "", errors.New("找不到这张图片的任务")
	}
	if source.AgentRunID == "" || source.PlanItemID == "" {
		return "", errors.New("只能优化智能生图的结果")
	}
	if source.Status != "succeeded" {
		return "", errors.New("只能优化已完成的图片")
	}
	owned := false
	for _, id := range source.AssetIDs {
		owned = owned || id == in.AssetID
	}
	if !owned {
		return "", errors.New("图片不属于这个任务")
	}
	var asset Asset
	if e := s.store.Get("asset", in.AssetID, &asset); e != nil || asset.TaskID != source.ID {
		return "", errors.New("原图不可用，请刷新后重试")
	}
	tasks, e := List[Task](s.store, "task")
	if e != nil {
		return "", e
	}
	for _, t := range tasks {
		if t.AgentRunID == source.AgentRunID && t.PlanItemID == source.PlanItemID && active(t.Status) {
			return "", errors.New("这张图片正在生成或优化，请等待完成")
		}
	}
	var c Connection
	if e := s.store.Get("connection", source.Connection.ID, &c); e != nil {
		return "", errors.New("原生图连接已删除，请重新配置后再优化")
	}
	if normalizeConnection(c).Kind != "image" {
		return "", errors.New("请选择生图中转站")
	}
	if _, e := s.key(c.Credential); e != nil {
		return "", e
	}
	t := source
	t.ID = NewID()
	t.CreatedAt = Now()
	t.UpdatedAt = t.CreatedAt
	t.Status = "queued"
	t.Error = ""
	t.Results = nil
	t.AssetIDs = []string{}
	t.Diagnostic = GenerationDiagnostic{}
	t.Connection = c
	t.References = []string{in.AssetID}
	// The suite's sample goes along as the second image, so the edit has a picture of
	// the cuff detail to draw instead of only words.
	t.SampleAssetID = ""
	var sample Asset
	if source.SampleAssetID != "" && s.store.Get("asset", source.SampleAssetID, &sample) == nil {
		t.SampleAssetID = source.SampleAssetID
		t.References = append(t.References, source.SampleAssetID)
	} else {
		t.SampleItem = ""
	}
	t.AutoRetryOf, t.AutoRetriedAs = "", ""
	t.PolicyRetryOf, t.PolicyRetriedAs, t.PolicyStep, t.Rewording = "", "", 0, false
	t.RefinedFrom = source.ID
	t.RefineInstruction = in.Instruction
	t.PreviousAssetIDs = nil
	t.Prompt = refinePrompt(baseTaskPrompt(source), in.Instruction, t.SampleAssetID != "")
	if e := s.ensureCanGenerateLocked(1, c.ID); e != nil {
		return "", e
	}
	t.Trial = s.trialLocked()
	batch, e := s.store.Submit(in.RequestID, source.BatchID, []Task{t})
	if e != nil {
		return "", e
	}
	// The instruction is a prompt and is not logged.
	_ = s.store.appendLog("info", "agent", source.AgentRunID, "已提交图片优化请求（基于已完成图片，原图保留）")
	s.signal()
	return batch, nil
}

// ListConnectionModels returns the model IDs of a saved connection for selection
// lists. It only performs GET /models with the stored key: no generation, no cost.
func (s *Service) ListConnectionModels(id string) ([]string, error) {
	s.mu.Lock()
	var c Connection
	e := s.store.Get("connection", id, &c)
	var key string
	if e == nil {
		key, e = s.key(c.Credential)
	}
	s.mu.Unlock()
	if e != nil {
		return nil, e
	}
	// Same limit as the connection test: some relays answer /models slowly.
	ctx, cancel := context.WithTimeout(s.ctx, probeTimeout)
	defer cancel()
	ids, e := s.provider.Models(ctx, normalizeConnection(c), key)
	if e != nil {
		// Fall back to the last list this relay returned (connection test, a
		// health check or an earlier lookup), so the picker is not left empty.
		if saved := s.savedModels(id); len(saved) > 0 {
			return saved, nil
		}
		return nil, e
	}
	sort.Strings(ids)
	s.saveModels(id, "", ids)
	return ids, nil
}

const modelListKind = "connection_models"

func (s *Service) savedModels(id string) []string {
	var ids []string
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.store.Get(modelListKind, id, &ids) != nil || len(ids) == 0 {
		if p, ok := s.probes[id]; ok && p.ok {
			ids = append([]string{}, p.models...)
			sort.Strings(ids)
		}
	}
	return ids
}

// saveModels remembers a relay's model list; it never holds a key. A non-empty
// base must match the saved connection (a test of edited, unsaved fields is skipped).
func (s *Service) saveModels(id, base string, ids []string) {
	if len(ids) == 0 {
		return
	}
	sorted := append([]string{}, ids...)
	sort.Strings(sorted)
	s.mu.Lock()
	defer s.mu.Unlock()
	var c Connection
	if s.store.Get("connection", id, &c) == nil && (base == "" || c.BaseURL == base) {
		_ = s.store.Put(modelListKind, id, sorted)
	}
}

const refinePromptMarker = "\n\n原图制作要求（只用于保持没有要求修改的部分；与本次修改冲突的内容作废，不要重新构图）：\n"

// refinePromptMarkerV1 is the marker of refine prompts saved before 0.6.11.
const refinePromptMarkerV1 = "\n\n原图制作要求（仅用于保持一致，不要重新构图）：\n"

// baseTaskPrompt drops the refine wrapper of earlier rounds so prompts do not nest.
func baseTaskPrompt(t Task) string {
	if t.RefinedFrom != "" {
		for _, marker := range []string{refinePromptMarker, refinePromptMarkerV1} {
			if _, base, ok := strings.Cut(t.Prompt, marker); ok {
				return base
			}
		}
	}
	return t.Prompt
}

// refinePrompt puts the requested change ahead of everything that must stay. The
// original prompt still describes the old state (and earlier wrappers listed text
// arrangement as unchangeable), so without an explicit priority the edit model
// keeps the image as it was. The original's sample line points at a reference
// position the edit no longer has; left in, the model took the image being edited
// as the sample and copied its cuff unchanged, so it is replaced by withSample's line.
func refinePrompt(base, instruction string, withSample bool) string {
	// Prompts saved before 0.6.13 told the model to tilt cuff text with the sleeve,
	// which argues against straightening it; use the current wording instead.
	base = strings.ReplaceAll(base, p004TiltWording, p004LevelWording)
	if strings.HasPrefix(base, "样板图：参考图第") {
		if _, rest, ok := strings.Cut(base, "\n"); ok {
			base = rest
		}
	}
	head := "以提供的第 1 张图片为底图修改。\n本次修改（必须做到，改完后要能明显看出变化）：" + instruction + "\n本次修改优先于原图现状和下面的原图制作要求，二者冲突时按本次修改画。为做到本次修改，可以一并调整与它直接相关的局部，例如要把袖口文字摆正时，把有刺绣的手臂和袖子改成在画面里竖直下垂、袖口接缝水平。除此以外保持完全一致：商品造型与比例、图案、文字内容、字体、颜色、人物、背景和构图都不要变化。"
	if withSample {
		head += "\n" + refineSampleLine
	}
	return head + refinePromptMarker + base
}

const refineSampleLine = "第 2 张图片是本套的细节样板（袖口等局部特写），不是底图，只用来看这些细节应该画成什么样：图标和文字的排列方式、左右顺序、文字方向、字体、颜色和相对大小照样板画；不照它的衣服颜色、构图和背景。"
