package core

import (
	"imageworkbench/internal/i18n"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestMoodLibraryIsComplete(t *testing.T) {
	lib := testService(t).ListMoods()
	if len(lib.Weather) != 7 || len(lib.Time) != 4 || len(lib.Distance) != 3 || len(lib.Expectation) != 3 || len(lib.Renderings) != 2 || len(lib.Nudges) != 6 {
		t.Fatal(len(lib.Weather), len(lib.Time), len(lib.Distance), len(lib.Expectation), len(lib.Renderings), len(lib.Nudges))
	}
	for _, group := range [][]MoodOption{lib.Weather, lib.Time, lib.Distance, lib.Expectation, lib.Renderings, lib.Nudges} {
		seen := map[string]bool{}
		for _, o := range group {
			if o.ID == "" || seen[o.ID] || o.Name == "" || o.Short == "" || o.Rules == "" {
				t.Fatalf("bad or duplicate option %+v", o)
			}
			seen[o.ID] = true
		}
	}
	for _, group := range [][]MoodOption{lib.Weather, lib.Time, lib.Distance, lib.Expectation, lib.Renderings} {
		for _, o := range group {
			if o.Phrase == "" || o.Hint == "" {
				t.Fatalf("%s has no phrase or fallback hint", o.Name)
			}
		}
	}
	lift, _ := findMood(moodExpectation, "lift")
	if !strings.Contains(lift.Rules, "不要改成相反的天气") || !strings.Contains(lift.Rules, "不要画成晴朗明亮的画面") {
		t.Fatal("lifting must keep the weather", lift.Rules)
	}
	// The three expectations say different, drawable things.
	soothe, _ := findMood(moodExpectation, "soothe")
	resonate, _ := findMood(moodExpectation, "resonate")
	if !strings.Contains(soothe.Rules, "可以待的地方") || !strings.Contains(lift.Rules, "出口") || !strings.Contains(resonate.Rules, "不美化") {
		t.Fatal("expectations lost their core rule")
	}
	// Rules stay in the backend.
	data, _ := json.Marshal(lib)
	if strings.Contains(string(data), "明暗：") || strings.Contains(string(data), "rules") || strings.Contains(string(data), "hint") {
		t.Fatal("rules leaked to the frontend")
	}
	if got := moodLabel(MoodChoice{"drizzle", "night", "far", "soothe"}); got != "细雨 · 深夜 · 很远 · 缓一缓" {
		t.Fatal(got)
	}
}

// moodRelay is an image gateway that records each prompt and the number of
// reference images (edits) it was sent.
type moodCall struct {
	prompt string
	images int
}

func moodRelay(t *testing.T) (*httptest.Server, *[]moodCall, *sync.Mutex) {
	var mu sync.Mutex
	calls := []moodCall{}
	png := base64.StdEncoding.EncodeToString(tinyPNG())
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := moodCall{}
		if strings.HasSuffix(r.URL.Path, "/images/edits") {
			r.ParseMultipartForm(8 << 20)
			call = moodCall{r.FormValue("prompt"), len(r.MultipartForm.File["image"]) + len(r.MultipartForm.File["image[]"])}
		} else {
			var m map[string]any
			json.NewDecoder(r.Body).Decode(&m)
			call.prompt, _ = m["prompt"].(string)
		}
		mu.Lock()
		calls = append(calls, call)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"data": []Output{{Base64: png}}})
	}))
	t.Cleanup(up.Close)
	return up, &calls, &mu
}

func moodInput(c Connection) GenerateInput {
	in := request(c)
	in.Prompts = []string{"一只白色陶瓷杯，杯身印着“晨光”"}
	in.Mood = &MoodChoice{Weather: "drizzle", Time: "night", Distance: "far", Expectation: "lift"}
	return in
}

func TestMoodSubmitHoldsAndRewritesBeforeSending(t *testing.T) {
	s := testService(t)
	up, calls, mu := moodRelay(t)
	requests, lmu := conceptLLM(t, s, "夜里细雨中的远景，一只白色陶瓷杯，杯身印着“晨光”，远处云缝透下一束光")
	c := saveConnection(t, s, up.URL)
	in := moodInput(c)
	in.Compare = true
	in.Count = 5 // a comparison is one image per rendering
	if _, e := s.Submit(in); e != nil {
		t.Fatal(e)
	}
	tasks, _ := List[Task](s.store, "task")
	if len(tasks) != 2 {
		t.Fatal("a comparison is two images", len(tasks))
	}
	renderings := map[string]bool{}
	for _, x := range tasks {
		renderings[x.Rendering] = true
		if x.Mood != "细雨 · 深夜 · 很远 · 拉一把" || x.BasePrompt != in.Prompts[0] || x.MoodChoice == nil || x.MoodChoice.Expectation != "lift" {
			t.Fatalf("mood not kept on the task: %+v", x)
		}
		if !x.Rewording || x.RewordFor != rewordMood {
			// The rewrite may already be done; it must never have been sent before.
			mu.Lock()
			n := len(*calls)
			mu.Unlock()
			if n != 0 {
				t.Fatal("sent before the rewrite")
			}
		}
	}
	if !renderings[renderingRealistic] || !renderings[renderingPainterly] {
		t.Fatal("expected one realistic and one painterly image", renderings)
	}
	waitReworded(t, s)
	s.stepNext()
	s.stepNext()
	tasks, _ = List[Task](s.store, "task")
	for _, x := range tasks {
		if x.Status != "succeeded" || !strings.Contains(x.Prompt, "云缝透下一束光") || x.BasePrompt == x.Prompt {
			t.Fatalf("rewritten prompt not sent: %+v", x)
		}
	}
	mu.Lock()
	sent := len(*calls)
	mu.Unlock()
	lmu.Lock()
	reqs := append([]string{}, (*requests)...)
	lmu.Unlock()
	if sent != 2 || len(reqs) != 2 {
		t.Fatal("expected two images and two rewrites", sent, len(reqs))
	}
	m, _ := resolveMood(*in.Mood)
	for _, req := range reqs {
		for _, rules := range []string{m.weather.Rules, m.time.Rules, m.distance.Rules, m.expectation.Rules} {
			if !strings.Contains(req, rules) {
				t.Fatal("request misses a mood rule:", req)
			}
		}
		if !strings.Contains(req, "一只白色陶瓷杯") {
			t.Fatal("request misses the prompt", req)
		}
	}
	realistic, _ := findMood(moodRenderings, renderingRealistic)
	painterly, _ := findMood(moodRenderings, renderingPainterly)
	joined := strings.Join(reqs, "\n")
	if !strings.Contains(joined, realistic.Rules) || !strings.Contains(joined, painterly.Rules) {
		t.Fatal("each rendering must reach its rewrite")
	}
	llmCalls, _ := List[LLMCall](s.store, "llm_call")
	if len(llmCalls) != 2 || llmCalls[0].Stage != i18n.T(i18n.ZhCN, "mood.llmStage") {
		t.Fatalf("rewrites not in the LLM monitor: %+v", llmCalls)
	}
	var logged, leaked int
	s.store.db.QueryRow(`SELECT COUNT(*) FROM app_logs WHERE message=?`, "已按心境改写提示词").Scan(&logged)
	s.store.db.QueryRow(`SELECT COUNT(*) FROM app_logs WHERE message LIKE ? OR message LIKE ?`, "%陶瓷杯%", "%晨光%").Scan(&leaked)
	if logged != 2 || leaked != 0 {
		t.Fatal("expected log lines without the prompt", logged, leaked)
	}
}

func TestMoodAndConceptShareOneRewrite(t *testing.T) {
	s := testService(t)
	up, _, _ := moodRelay(t)
	requests, lmu := conceptLLM(t, s, "灰色巨墙前的细雨夜景，一只白色陶瓷杯，杯身印着“晨光”")
	c := saveConnection(t, s, up.URL)
	in := moodInput(c)
	in.Concept = &ConceptChoice{Form: "mega", Stance: "romanticism", Strength: 1}
	in.ConceptLabels = []string{"ignored"}
	if _, e := s.Submit(in); e != nil {
		t.Fatal(e)
	}
	tasks := waitReworded(t, s)
	if len(tasks) != 1 || tasks[0].Concept != "巨物 × 浪漫主义" || tasks[0].ConceptChoice == nil {
		t.Fatalf("concept not kept: %+v", tasks)
	}
	lmu.Lock()
	defer lmu.Unlock()
	form, _ := findConcept(conceptForms, "mega")
	stance, _ := findConcept(conceptStances, "romanticism")
	m, _ := resolveMood(*in.Mood)
	if len(*requests) != 1 {
		t.Fatal("mood and concept must be one rewrite", len(*requests))
	}
	req := (*requests)[0]
	if !strings.Contains(req, form.Rules) || !strings.Contains(req, stance.Rules) || !strings.Contains(req, m.expectation.Rules) || !strings.Contains(req, "概念管形体") {
		t.Fatal("request misses the concept or the mood:", req)
	}
}

func TestMoodAllowsAnEmptyPrompt(t *testing.T) {
	s := testService(t)
	up, _, _ := moodRelay(t)
	requests, lmu := conceptLLM(t, s, "细雨夜里远处一盏路灯下的空长椅")
	c := saveConnection(t, s, up.URL)
	in := moodInput(c)
	in.Prompts = []string{}
	if _, e := s.Submit(in); e != nil {
		t.Fatal(e)
	}
	tasks := waitReworded(t, s)
	if len(tasks) != 1 || tasks[0].Status != "queued" || tasks[0].Prompt != "细雨夜里远处一盏路灯下的空长椅" || tasks[0].BasePrompt != "" {
		t.Fatalf("%+v", tasks)
	}
	lmu.Lock()
	req := (*requests)[0]
	lmu.Unlock()
	if !strings.Contains(req, "用户没有写提示词") || !strings.Contains(req, "不要加入商品") {
		t.Fatal(req)
	}
	// Without a mood an empty prompt is still refused.
	plain := request(c)
	plain.Prompts = []string{"  "}
	if _, e := s.Submit(plain); e == nil {
		t.Fatal("empty prompt accepted without a mood")
	}
}

func TestMoodCompareRules(t *testing.T) {
	s := testService(t)
	c := saveConnection(t, s, "http://127.0.0.1:1")
	in := moodInput(c)
	in.Compare = true
	in.Prompts = []string{"第一条", "第二条"}
	if _, e := s.Submit(in); e == nil || !strings.Contains(e.Error(), "一条提示词") {
		t.Fatal("comparison with several prompts accepted", e)
	}
	plain := request(c)
	plain.Compare = true
	if _, e := s.Submit(plain); e == nil || !strings.Contains(e.Error(), "心境") {
		t.Fatal("comparison without a mood accepted", e)
	}
	plain.Compare = false
	plain.Concept = &ConceptChoice{Form: "mega", Stance: "romanticism"}
	if _, e := s.Submit(plain); e == nil {
		t.Fatal("a picked concept without a mood accepted")
	}
	bad := moodInput(c)
	bad.Mood.Weather = "nope"
	if _, e := s.Submit(bad); e == nil {
		t.Fatal("unknown mood option accepted")
	}
	if all, _ := List[Task](s.store, "task"); len(all) != 0 {
		t.Fatal("rejected submissions created tasks", len(all))
	}
}

func TestMoodRewriteLosingQuotedTextIsNotSent(t *testing.T) {
	s := testService(t)
	up, calls, mu := moodRelay(t)
	conceptLLM(t, s, "细雨夜里的一只陶瓷杯")
	c := saveConnection(t, s, up.URL)
	if _, e := s.Submit(moodInput(c)); e != nil {
		t.Fatal(e)
	}
	tasks := waitReworded(t, s)
	s.stepNext()
	if tasks[0].Status != "failed" || !strings.Contains(tasks[0].Error, "没有发送、不计费") || !strings.Contains(tasks[0].Error, "晨光") {
		t.Fatalf("%+v", tasks[0])
	}
	mu.Lock()
	defer mu.Unlock()
	if len(*calls) != 0 {
		t.Fatal("a failed rewrite was sent")
	}
}

func TestMoodFallsBackWithoutLLM(t *testing.T) {
	s := testService(t)
	up, calls, mu := moodRelay(t)
	c := saveConnection(t, s, up.URL)
	in := moodInput(c)
	in.Concept = &ConceptChoice{Form: "ruin", Stance: "wabisabi"}
	if _, e := s.Submit(in); e != nil {
		t.Fatal(e)
	}
	tasks, _ := List[Task](s.store, "task")
	x := tasks[0]
	if x.Rewording || !strings.HasPrefix(x.Prompt, in.Prompts[0]+"\n画面气氛：") || !strings.Contains(x.Prompt, "出口") || !strings.Contains(x.Prompt, "废墟 × 侘寂") || x.Mood == "" {
		t.Fatalf("fallback points missing: %+v", x)
	}
	s.stepNext()
	mu.Lock()
	defer mu.Unlock()
	if len(*calls) != 1 || (*calls)[0].prompt != x.Prompt {
		t.Fatal("fallback prompt not sent as it is", *calls)
	}
	// An empty prompt still gets a subject and the points.
	in = moodInput(c)
	in.Prompts = nil
	if _, e := s.Submit(in); e != nil {
		t.Fatal(e)
	}
	empty := findTask(s, func(x Task) bool { return x.BasePrompt == "" })
	if !strings.HasPrefix(empty.Prompt, "一处与下面气氛相符的风景") {
		t.Fatal(empty.Prompt)
	}
}

func TestNudgeMoodImage(t *testing.T) {
	s := testService(t)
	up, calls, mu := moodRelay(t)
	c := saveConnection(t, s, up.URL)
	in := moodInput(c)
	in.Compare = true
	if _, e := s.Submit(in); e != nil {
		t.Fatal(e)
	}
	s.stepNext()
	s.stepNext()
	source := findTask(s, func(x Task) bool { return x.Rendering == renderingPainterly })
	if source.Status != "succeeded" || len(source.AssetIDs) != 1 {
		t.Fatalf("%+v", source)
	}
	plain := request(c)
	plain.Prompts = []string{"普通的一张"}
	s.Submit(plain)
	s.stepNext()
	other := findTask(s, func(x Task) bool { return x.Mood == "" })

	nudge := NudgeInput{RequestID: "nudge-1", TaskID: source.ID, AssetID: source.AssetIDs[0], Direction: "quieter"}
	for _, bad := range []NudgeInput{
		{RequestID: "", TaskID: source.ID, AssetID: source.AssetIDs[0], Direction: "quieter"},
		{RequestID: "b1", TaskID: source.ID, AssetID: source.AssetIDs[0], Direction: "nope"},
		{RequestID: "b2", TaskID: source.ID, AssetID: other.AssetIDs[0], Direction: "quieter"},
		{RequestID: "b3", TaskID: other.ID, AssetID: other.AssetIDs[0], Direction: "quieter"},
	} {
		if _, e := s.NudgeMoodImage(bad); e == nil {
			t.Fatal("invalid nudge accepted", bad)
		}
	}
	if batch, e := s.NudgeMoodImage(nudge); e != nil || batch != source.BatchID {
		t.Fatal(batch, e)
	}
	if _, e := s.NudgeMoodImage(nudge); e != nil {
		t.Fatal("repeated click should return the same submission", e)
	}
	if tasks, _ := List[Task](s.store, "task"); len(tasks) != 4 {
		t.Fatal("expected exactly one new task", len(tasks))
	}
	next := findTask(s, func(x Task) bool { return x.NudgeOf == source.ID })
	if next.NudgeOf != source.ID || next.Nudge != "更安静" || next.Mood != source.Mood || next.Rendering != source.Rendering || next.MoodChoice == nil || next.Rewording {
		t.Fatalf("nudge did not inherit the mood: %+v", next)
	}
	if len(next.References) != 1 || next.References[0] != source.AssetIDs[0] || !strings.Contains(next.Prompt, "只改这一点：让画面更安静") || !strings.Contains(next.Prompt, source.Prompt) {
		t.Fatalf("%+v", next)
	}
	s.stepNext()
	mu.Lock()
	last := (*calls)[len(*calls)-1]
	mu.Unlock()
	if last.images != 1 || !strings.Contains(last.prompt, "让画面更安静") {
		t.Fatal("nudge not sent as an edit of the image", last)
	}
	// A nudge of a nudge keeps one wrapper around the original prompt.
	next = findTask(s, func(x Task) bool { return x.ID == next.ID })
	if _, e := s.NudgeMoodImage(NudgeInput{RequestID: "nudge-2", TaskID: next.ID, AssetID: next.AssetIDs[0], Direction: "closer"}); e != nil {
		t.Fatal(e)
	}
	second := findTask(s, func(x Task) bool { return x.NudgeOf == next.ID })
	if strings.Count(second.Prompt, nudgePromptMarker) != 1 || strings.Contains(second.Prompt, "更安静") || second.Nudge != "更近" {
		t.Fatal(second.Prompt)
	}
}

func TestRetryOfAMoodImageKeepsTheMood(t *testing.T) {
	s := testService(t)
	up, _, _ := moodRelay(t)
	c := saveConnection(t, s, up.URL)
	if _, e := s.Submit(moodInput(c)); e != nil {
		t.Fatal(e)
	}
	s.stepNext()
	tasks, _ := List[Task](s.store, "task")
	if _, e := s.Retry(tasks[0].ID); e != nil {
		t.Fatal(e)
	}
	tasks, _ = List[Task](s.store, "task")
	if len(tasks) != 2 || tasks[0].Mood != tasks[1].Mood || tasks[0].BatchID == tasks[1].BatchID || tasks[0].Prompt != tasks[1].Prompt {
		t.Fatalf("%+v", tasks)
	}
}

// findTask returns the one stored task that matches.
func findTask(s *Service, match func(Task) bool) Task {
	tasks, _ := List[Task](s.store, "task")
	for _, x := range tasks {
		if match(x) {
			return x
		}
	}
	return Task{}
}

func TestGuideModeIsRemembered(t *testing.T) {
	s := testService(t)
	if s.GetGuideMode() {
		t.Fatal("guide mode must start off")
	}
	if e := s.SetGuideMode(true); e != nil || !s.GetGuideMode() {
		t.Fatal(e)
	}
	s.SetGuideMode(false)
	if s.GetGuideMode() {
		t.Fatal("guide mode not turned off")
	}
}
