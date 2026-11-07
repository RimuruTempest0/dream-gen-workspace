package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

// ---- manual / automatic smart generation (0.7.4, self-answering since 0.7.5) --
//
// Manual mode is the original flow: when facts are missing the Agent asks, and
// the run waits for the user's answers. Automatic mode asks the same questions,
// but nobody answers them: the Agent answers each one itself the way the user
// would, records where the answer came from, and goes on to save the plan.

const (
	smartModePrefKey = "smart-mode"
	smartModeManual  = "manual"
	smartModeAuto    = "auto"
	// maxAutoContinues bounds how often an automatic run is continued by itself.
	maxAutoContinues = 2
	// answerHabitCount is how many recent manual answers an automatic run learns from.
	answerHabitCount = 10
)

// SmartMode is the mode new smart-generation requests start in.
func (s *Service) SmartMode() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var v string
	if s.store.Get("preferences", smartModePrefKey, &v) != nil || v != smartModeAuto {
		return smartModeManual
	}
	return v
}

func (s *Service) SetSmartMode(v string) error {
	if v != smartModeManual && v != smartModeAuto {
		return errors.New("制作模式无效")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.store.Put("preferences", smartModePrefKey, v)
}

// autoModeRule replaces the manual rule about asking the user.
const autoModeRule = `本次为全自动模式：用户不会回答问题，也不会审阅方案。像手动模式一样，只针对本次指令真正需要、而现有素材、商品资料、Skill 附件都没有提供的事实提出问题，用 request_information 记下；然后不要结束本轮，像用户本人那样逐题自己回答，用 answer_questions 记录每题的答案、依据和一句理由，再继续规划，把这些取值写进 locked，并用 save_suite_plan 保存完整方案。
回答前先理解用户意图：用户说保留、不变、沿用的，严格照原商品链接和参考图；说换成、改成、新增的，用新内容；没有提到的，照原链接。
逐题按顺序找答案：
1. 指令或补充说明里已经写了的，照用（basis=instruction）。
2. 原商品页文字、参考图或 Skill 附件能看出来的，严格照原链接（basis=source），例如商品页列出的可选线色、原图里的那款配件。
3. 可以自由选择的设计（颜色、线色、背景、道具等），选与原设计风格一致、在商品页可选范围内的方案，写明理由（basis=choice）。
4. answerHabits 显示用户对同类问题有固定的判断习惯时，按习惯回答（basis=habit）。
5. 只有用户本人知道、以上都推不出的事实（例如新的名字、真实尺寸），选一个合理、容易替换的值（basis=verify），供用户事后核对。
answerHabits 是用户在手动模式下最近的回答，只用来理解判断习惯：什么时候回答“随意”、什么时候坚持照原链接、什么内容由用户本人指定。其中的具体取值属于当时那次制作，除非本次确实是同一件事，否则不要照抄；与本次指令冲突时以本次指令为准。
已有问题的答案和已有方案见上下文。`

// autoSelfAnswer replies to request_information in automatic mode.
const autoSelfAnswer = "已记录问题。全自动模式没有用户回答：不要结束本轮，按判断顺序逐题自己回答，用 answer_questions 记录（每题 answer、basis、reason），然后继续规划并保存完整方案。"

// answerBases are the allowed sources of a self-answer; verify marks a value the
// user should check.
var answerBases = map[string]bool{"instruction": true, "source": true, "choice": true, "habit": true, "verify": true}

func answerQuestionsTool() map[string]any {
	answer := toolSchema(map[string]any{
		"id":     textProperty("request_information 里的问题 ID"),
		"answer": textProperty("答案，写成可以直接用于规划的具体取值"),
		"basis":  map[string]any{"type": "string", "enum": []string{"instruction", "source", "choice", "habit", "verify"}, "description": "依据：instruction 指令里已有；source 原商品链接、参考图或附件能看出；choice 可自由选择、按风格选定；habit 按用户以往的回答习惯；verify 推不出、取了合理值需用户核对"},
		"reason": textProperty("一句话理由，100 字以内"),
	}, "id", "answer", "basis", "reason")
	return map[string]any{"name": "answer_questions", "description": "全自动模式：自己回答 request_information 记下的问题，然后继续规划并保存方案。", "inputSchema": toolSchema(map[string]any{"answers": map[string]any{"type": "array", "items": answer, "minItems": 1, "maxItems": 30}}, "answers")}
}

// answerQuestions records the Agent's own answers. Answers given are kept even
// when some required questions are still open; the error then names those.
func (b *agentBridge) answerQuestions(r AgentRun, args json.RawMessage) ([]any, error) {
	if !r.Input.AutoMode {
		return nil, errors.New("只有全自动模式可以自行回答问题")
	}
	var v struct {
		Answers []struct {
			ID     string `json:"id"`
			Answer string `json:"answer"`
			Basis  string `json:"basis"`
			Reason string `json:"reason"`
		} `json:"answers"`
	}
	if e := json.Unmarshal(args, &v); e != nil {
		return nil, e
	}
	var open []string
	e := b.service.updateAgent(b.runID, "自行回答问题", func(run *AgentRun) error {
		byID := map[string]int{}
		for i, q := range run.Questions {
			byID[q.ID] = i
		}
		for _, a := range v.Answers {
			i, ok := byID[a.ID]
			if !ok {
				return fmt.Errorf("没有这个问题：%s（只能回答 request_information 记下的问题）", a.ID)
			}
			if strings.TrimSpace(a.Answer) == "" || len(a.Answer) > 2000 || utf8.RuneCountInString(a.Reason) > 300 {
				return fmt.Errorf("问题 %s 的答案为空或过长", a.ID)
			}
			if !answerBases[a.Basis] {
				return fmt.Errorf("问题 %s 的 basis 无效", a.ID)
			}
			q := &run.Questions[i]
			q.Answer, q.AnsweredBy, q.Basis, q.Reason = strings.TrimSpace(a.Answer), "agent", a.Basis, strings.TrimSpace(a.Reason)
		}
		open = openQuestions(run.Questions)
		return nil
	})
	if e != nil {
		return nil, e
	}
	if len(open) > 0 {
		return nil, fmt.Errorf("已记录这些答案，还有问题没回答：%s。请继续用 answer_questions 回答", strings.Join(open, "；"))
	}
	return mcpText(map[string]any{"answered": len(v.Answers), "next": "继续规划，把这些取值写进 locked，并用 save_suite_plan 保存完整方案"}), nil
}

// openQuestions lists required questions without an answer as "id：label".
func openQuestions(qs []AgentQuestion) []string {
	open := []string{}
	for _, q := range qs {
		if q.Required && strings.TrimSpace(q.Answer) == "" {
			open = append(open, q.ID+"："+cutProductText(q.Label, 120))
		}
	}
	return open
}

// recentManualAnswersLocked returns the user's latest answers from manual runs,
// newest first: what an automatic run learns the user's way of deciding from.
// Answers the app filled in or the Agent gave itself are left out.
func (s *Service) recentManualAnswersLocked() []AnswerHabit {
	runs, e := List[AgentRun](s.store, "agent_run")
	if e != nil {
		return nil
	}
	out := []AnswerHabit{}
	for _, r := range runs { // newest first
		if r.Input.AutoMode {
			continue
		}
		for _, q := range r.Questions {
			answer := strings.TrimSpace(q.Answer)
			if answer == "" || q.AnsweredBy != "" || q.ID == "product-material" {
				continue
			}
			out = append(out, AnswerHabit{Question: cutProductText(q.Label, 300), Answer: cutProductText(answer, 500)})
			if len(out) == answerHabitCount {
				return out
			}
		}
	}
	return out
}

// questionRule is the prompt's rule on missing facts: unchanged in manual mode.
func questionRule(r AgentRun) string {
	if r.Input.AutoMode {
		return autoModeRule
	}
	return `只询问本次指令真正需要且现有素材、商品资料、Skill附件未提供的事实。真实尺寸、指定名字、全部色卡、指定字体和上传填充色不得编造；可选字体参考不强制索取。产品外观要求冲突时集中提问。调用 request_information 后结束本轮，资料齐全前不交付完整方案。
已有问题的答案和已有方案见上下文；已补充的信息不能重复索要。可以保留部分方案，但不能假装信息完整。`
}

// askingStyle is the prompt's line on how to ask: manual mode only.
func askingStyle(r AgentRun) string {
	if r.Input.AutoMode {
		return ""
	}
	return "补充问题用简短清晰的客户用语，一次集中提出，已提供的资料不得重复索取。\n"
}

// autoContinueLocked prepares another attempt of an automatic run that ended
// without submitting a complete set (questions it did not answer, no plan or a
// failed attempt) and returns its instruction; "" leaves the run as it is.
func autoContinueLocked(r *AgentRun) string {
	if !r.Input.AutoMode || r.BatchID != "" || (r.Status != "failed" && r.Status != "needs_input") || r.AutoContinues >= maxAutoContinues {
		return ""
	}
	r.AutoContinues++
	reason, instruction := r.Error, ""
	if open := openQuestions(r.Questions); r.Status == "needs_input" && len(open) > 0 {
		reason = fmt.Sprintf("还有 %d 个问题没有自行回答", len(open))
		instruction = fmt.Sprintf("（全自动模式第 %d 次自动继续）上一轮记下了问题，但没有自己回答：%s。用户不会回答。请先按判断顺序逐题回答，用 answer_questions 记录，再继续规划并用 save_suite_plan 保存完整方案。", r.AutoContinues, strings.Join(open, "；"))
	} else {
		instruction = fmt.Sprintf("（全自动模式第 %d 次自动继续）上一轮没有保存完整套图方案：%s。用户不会回答问题，也不会审阅；还缺的资料按判断顺序自己回答，再用 save_suite_plan 保存完整方案。", r.AutoContinues, reason)
	}
	r.Activity = append(r.Activity, ActivityEntry{At: Now(), Kind: "warn", Text: cutActivityText(fmt.Sprintf("全自动模式：上一轮没有完成整套方案（%s），自动继续制作（第 %d 次）", reason, r.AutoContinues))})
	r.Status, r.Stage, r.Error = "analyzing", "自动继续制作", ""
	return instruction
}
