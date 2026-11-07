package core

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func autoInput(llm, images Connection) AgentInput {
	in := agentInput(llm, images)
	in.AutoMode = true
	return in
}

func toolNames(r AgentRun) string {
	names := []string{}
	for _, t := range toolsFor(r) {
		names = append(names, t.(map[string]any)["name"].(string))
	}
	return strings.Join(names, ",")
}

func TestSmartModePreference(t *testing.T) {
	s := testService(t)
	if s.SmartMode() != "manual" {
		t.Fatal("default is not manual", s.SmartMode())
	}
	if e := s.SetSmartMode("auto"); e != nil || s.SmartMode() != "auto" {
		t.Fatal(e, s.SmartMode())
	}
	if s.SetSmartMode("other") == nil {
		t.Fatal("invalid mode accepted")
	}
}

// Manual mode is unchanged: the question rule and tool stay, there is no
// self-answer tool, and a run with open questions waits for the user.
func TestManualModeStillWaitsForAnswers(t *testing.T) {
	s := testService(t)
	llm := saveLLM(t, s, "https://example.com/anthropic")
	images := saveConnection(t, s, "https://example.com/v1")
	var calls atomic.Int32
	s.agentRunner = func(ctx context.Context, in agentInvocation) error {
		calls.Add(1)
		r := invocationRun(t, in)
		if !strings.Contains(in.Prompt, "调用 request_information 后结束本轮，资料齐全前不交付完整方案。\n已有问题的答案和已有方案见上下文；已补充的信息不能重复索要。可以保留部分方案，但不能假装信息完整。\n自动制作") ||
			!strings.Contains(in.Prompt, "再次确认生图。补充问题用简短清晰的客户用语") || strings.Contains(in.Prompt, "全自动模式") || strings.Contains(in.Prompt, "answerHabits") {
			return errors.New("manual prompt changed")
		}
		if names := toolNames(r); !strings.Contains(names, "request_information") || strings.Contains(names, "answer_questions") || slices.Contains(in.Args, "mcp__workbench__answer_questions") {
			return errors.New("manual tools changed: " + names)
		}
		b := &agentBridge{service: s, runID: r.ID, ctx: context.Background()}
		args, _ := json.Marshal(map[string]any{"questions": []AgentQuestion{{ID: "sizes", Label: "请补充真实尺寸", Required: true}}})
		if out, e := b.callTool("request_information", args); e != nil || !strings.Contains(out[0].(map[string]any)["text"].(string), "等待用户补充") {
			return errors.New("manual question not waiting")
		}
		answer, _ := json.Marshal(map[string]any{"answers": []map[string]string{{"id": "sizes", "answer": "M", "basis": "choice", "reason": "x"}}})
		if _, e := b.callTool("answer_questions", answer); e == nil {
			return errors.New("manual run answered its own question")
		}
		emitAgentSuccess(in)
		return nil
	}
	id, e := s.StartSmartGeneration(agentInput(llm, images), "manual-start")
	if e != nil {
		t.Fatal(e)
	}
	s.waitAgent()
	r, _ := s.GetAgentRun(id)
	if r.Status != "needs_input" || calls.Load() != 1 || r.AutoContinues != 0 || r.Questions[0].Answer != "" {
		t.Fatal(r.Status, r.Error, calls.Load(), r.Questions)
	}
}

var screenshotQuestions = []AgentQuestion{
	{ID: "pet", Label: "新宠物肖像的品种和毛色", Required: true},
	{ID: "name", Label: "绣在袜口上的新英文名字", Required: true},
	{ID: "thread", Label: "新的名字绣线颜色", Required: true},
	{ID: "hat", Label: "保留的圣诞帽是指参考图里红绿迷你圣诞袜帽这一款吗", Required: true},
}

func selfAnswer(id, answer, basis string) map[string]string {
	return map[string]string{"id": id, "answer": answer, "basis": basis, "reason": "依据说明"}
}

// Automatic mode asks the same questions, answers each one itself with its basis,
// and submits without waiting.
func TestAutoModeAnswersItsOwnQuestions(t *testing.T) {
	s := testService(t)
	llm := saveLLM(t, s, "https://example.com/anthropic")
	images := saveConnection(t, s, "https://example.com/v1")
	var calls atomic.Int32
	var failure error
	s.agentRunner = func(ctx context.Context, in agentInvocation) error {
		calls.Add(1)
		r := invocationRun(t, in)
		fail := func(msg string) error { failure = errors.New(msg); return failure }
		if !strings.Contains(in.Prompt, autoModeRule) || strings.Contains(in.Prompt, "调用 request_information 后结束本轮") || strings.Contains(in.Prompt, "补充问题用简短清晰的客户用语") {
			return fail("automatic prompt")
		}
		if names := toolNames(r); !strings.Contains(names, "request_information") || !strings.Contains(names, "answer_questions") {
			return fail("tools: " + names)
		}
		// Claude Code must be allowed the tool, or it asks the model for permission.
		if !slices.Contains(in.Args, "mcp__workbench__answer_questions") {
			return fail("self-answer tool not allowed for Claude Code")
		}
		b := &agentBridge{service: s, runID: r.ID, ctx: context.Background()}
		args, _ := json.Marshal(map[string]any{"questions": screenshotQuestions})
		out, e := b.callTool("request_information", args)
		if e != nil || !strings.Contains(out[0].(map[string]any)["text"].(string), "不要结束本轮") {
			return fail("question reply does not ask for self-answers")
		}
		call := func(answers ...map[string]string) error {
			raw, _ := json.Marshal(map[string]any{"answers": answers})
			_, e := b.callTool("answer_questions", raw)
			return e
		}
		if e := call(selfAnswer("nope", "x", "choice")); e == nil || !strings.Contains(e.Error(), "没有这个问题") {
			return fail("unknown question accepted")
		}
		if e := call(selfAnswer("pet", "金毛", "guess")); e == nil || !strings.Contains(e.Error(), "basis") {
			return fail("invalid basis accepted")
		}
		if e := call(selfAnswer("pet", "金毛", "instruction"), selfAnswer("name", "LUNA", "verify"), selfAnswer("thread", "白色", "choice")); e == nil || !strings.Contains(e.Error(), "hat：") {
			return fail("open question not named")
		}
		if e := call(selfAnswer("hat", "严格参考原链接的红绿迷你圣诞帽", "source")); e != nil {
			return fail(e.Error())
		}
		if e := saveRunnerPlan(s, r, simplePlan(12, []string{})); e != nil {
			return e
		}
		emitAgentSuccess(in)
		return nil
	}
	id, e := s.StartSmartGeneration(autoInput(llm, images), "auto-start")
	if e != nil {
		t.Fatal(e)
	}
	s.waitAgent()
	if failure != nil {
		t.Fatal(failure)
	}
	r, _ := s.GetAgentRun(id)
	tasks, _ := s.GetAgentTasks(id)
	if r.Status != "submitted" || len(tasks) != 12 || calls.Load() != 1 {
		t.Fatal(r.Status, r.Error, len(tasks), calls.Load())
	}
	bases := []string{}
	for _, q := range r.Questions {
		if q.AnsweredBy != "agent" || q.Answer == "" || q.Reason == "" {
			t.Fatalf("not self-answered: %+v", q)
		}
		bases = append(bases, q.Basis)
	}
	if strings.Join(bases, ",") != "instruction,verify,choice,source" {
		t.Fatal(bases)
	}
}

// Questions left unanswered are not filled in by the app: the run continues and
// asks the Agent to answer exactly those.
func TestAutoModeContinuesWhenQuestionsLeftOpen(t *testing.T) {
	s := testService(t)
	llm := saveLLM(t, s, "https://example.com/anthropic")
	images := saveConnection(t, s, "https://example.com/v1")
	var calls atomic.Int32
	var failure error
	s.agentRunner = func(ctx context.Context, in agentInvocation) error {
		r := invocationRun(t, in)
		b := &agentBridge{service: s, runID: r.ID, ctx: context.Background()}
		if calls.Add(1) == 1 {
			args, _ := json.Marshal(map[string]any{"questions": screenshotQuestions[1:2]})
			b.callTool("request_information", args)
		} else {
			if !strings.Contains(in.Prompt, "没有自己回答：name：") || r.Questions[0].Answer != "" {
				failure = errors.New("continuation does not name the open question, or it was filled in")
				return failure
			}
			raw, _ := json.Marshal(map[string]any{"answers": []map[string]string{selfAnswer("name", "LUNA", "verify")}})
			if _, e := b.callTool("answer_questions", raw); e != nil {
				return e
			}
			if e := saveRunnerPlan(s, r, simplePlan(12, []string{})); e != nil {
				return e
			}
		}
		emitAgentSuccess(in)
		return nil
	}
	id, e := s.StartSmartGeneration(autoInput(llm, images), "auto-open")
	if e != nil {
		t.Fatal(e)
	}
	s.waitAgent()
	if failure != nil {
		t.Fatal(failure)
	}
	r, _ := s.GetAgentRun(id)
	if r.Status != "submitted" || calls.Load() != 2 || r.AutoContinues != 1 || r.Questions[0].Basis != "verify" {
		t.Fatal(r.Status, r.Error, calls.Load(), r.AutoContinues, r.Questions)
	}
}

// A run that keeps ending without a plan continues by itself at most twice; a
// user's continue allows two more.
func TestAutoModeContinuesAtMostTwice(t *testing.T) {
	s := testService(t)
	llm := saveLLM(t, s, "https://example.com/anthropic")
	images := saveConnection(t, s, "https://example.com/v1")
	var calls atomic.Int32
	saveOn := int32(99)
	s.agentRunner = func(ctx context.Context, in agentInvocation) error {
		n := calls.Add(1)
		r := invocationRun(t, in)
		if n == atomic.LoadInt32(&saveOn) {
			if e := saveRunnerPlan(s, r, simplePlan(12, []string{})); e != nil {
				return e
			}
		}
		emitAgentSuccess(in)
		return nil
	}
	id, e := s.StartSmartGeneration(autoInput(llm, images), "auto-give-up")
	if e != nil {
		t.Fatal(e)
	}
	s.waitAgent()
	r, _ := s.GetAgentRun(id)
	if r.Status != "failed" || calls.Load() != 3 || r.AutoContinues != 2 || r.Error == "" {
		t.Fatal(r.Status, r.Error, calls.Load(), r.AutoContinues)
	}
	atomic.StoreInt32(&saveOn, 4)
	if _, e = s.ContinueSmartGeneration(AgentContinuation{RunID: id}, "auto-user-continue"); e != nil {
		t.Fatal(e)
	}
	s.waitAgent()
	if r, _ = s.GetAgentRun(id); r.Status != "submitted" || calls.Load() != 4 {
		t.Fatal(r.Status, r.Error, calls.Load())
	}
}

// An automatic run learns from the user's latest 10 manual answers, of any
// product, newest first; filled-in and self-given answers are not habits.
func TestAutoModeLearnsFromRecentManualAnswers(t *testing.T) {
	s := testService(t)
	put := func(r AgentRun) {
		// Records are listed by when they were saved; keep the saves apart.
		time.Sleep(20 * time.Millisecond)
		r.ID = NewID()
		if e := s.store.Put("agent_run", r.ID, r); e != nil {
			t.Fatal(e)
		}
	}
	older := AgentRun{}
	for i := 0; i < 12; i++ {
		older.Questions = append(older.Questions, AgentQuestion{ID: "q", Label: "旧问题", Answer: "旧回答"})
	}
	put(older)
	put(AgentRun{Input: AgentInput{AutoMode: true}, Questions: []AgentQuestion{{Label: "自动问题", Answer: "自动回答", AnsweredBy: "agent"}}})
	put(AgentRun{Questions: []AgentQuestion{
		{ID: "thread", Label: "名字线色？", Answer: "随意"},
		{ID: "hat", Label: "圣诞帽是哪款？", Answer: "严格参考原链接的圣诞帽"},
		{ID: "product-material", Label: "请上传商品参考图", Answer: "已补充商品素材或描述"},
		{ID: "empty", Label: "没回答的问题"},
		{ID: "self", Label: "模型答的", Answer: "x", AnsweredBy: "agent"},
	}})
	s.mu.Lock()
	habits := s.recentManualAnswersLocked()
	s.mu.Unlock()
	if len(habits) != 10 || habits[0].Answer != "随意" || habits[1].Answer != "严格参考原链接的圣诞帽" || habits[2].Answer != "旧回答" {
		t.Fatalf("%+v", habits)
	}
	for _, h := range habits {
		if h.Answer == "自动回答" || h.Answer == "x" || h.Answer == "已补充商品素材或描述" {
			t.Fatal("not a user's answer", h)
		}
	}
	// Only an automatic run carries them to the Agent.
	auto := AgentRun{Input: AgentInput{AutoMode: true}, AnswerHabits: habits}
	if len(newAgentContext(auto).AnswerHabits) != 10 {
		t.Fatal("habits missing from the automatic context")
	}
	auto.Input.AutoMode = false
	if newAgentContext(auto).AnswerHabits != nil {
		t.Fatal("manual context carries habits")
	}
}
