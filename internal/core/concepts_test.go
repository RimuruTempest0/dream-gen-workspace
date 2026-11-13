package core

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestConceptLibraryIsComplete(t *testing.T) {
	lib := testService(t).ListConcepts()
	if len(lib.Forms) != 12 || len(lib.Stances) != 12 || len(lib.Picks) != 8 {
		t.Fatal(len(lib.Forms), len(lib.Stances), len(lib.Picks))
	}
	seen := map[string]bool{}
	groups := map[string]int{}
	for _, c := range append(append([]Concept{}, lib.Forms...), lib.Stances...) {
		if c.ID == "" || seen[c.ID] || c.Name == "" || c.En == "" || c.Look == "" {
			t.Fatalf("bad or duplicate card %+v", c)
		}
		seen[c.ID] = true
		groups[c.Group]++
		for _, head := range []string{"尺度：", "材质：", "光线：", "构图：", "色彩：", "避免："} {
			if !strings.Contains(c.Rules, head) {
				t.Fatalf("%s has no %s line", c.Name, head)
			}
		}
	}
	if len(groups) != 8 {
		t.Fatal("expected four groups of forms and four of stances", groups)
	}
	for _, p := range lib.Picks {
		if _, ok := findConcept(lib.Forms, p.Form); !ok {
			t.Fatal("pick refers to a missing form", p)
		}
		if _, ok := findConcept(lib.Stances, p.Stance); !ok {
			t.Fatal("pick refers to a missing stance", p)
		}
	}
	// Rules stay in the backend: the frontend only gets the one-line look.
	data, _ := json.Marshal(lib)
	if strings.Contains(string(data), "尺度：") || strings.Contains(string(data), "rules") {
		t.Fatal("rules leaked to the frontend")
	}
}

// conceptLLM answers every request with reply and records the request texts.
func conceptLLM(t *testing.T, s *Service, reply string) (*[]string, *sync.Mutex) {
	var mu sync.Mutex
	requests := []string{}
	llm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		requests = append(requests, body.Messages[0].Content)
		mu.Unlock()
		json.NewEncoder(w).Encode(map[string]any{"content": []map[string]any{{"type": "text", "text": reply}}, "usage": map[string]any{"input_tokens": 300, "output_tokens": 80}})
	}))
	t.Cleanup(llm.Close)
	if _, e := s.SaveConnection(ConnectionInput{Kind: "llm", Name: "LLM 线路", BaseURL: llm.URL, Model: "test-llm", Key: "llm-key"}); e != nil {
		t.Fatal(e)
	}
	return &requests, &mu
}

func TestComposeConceptRewritesWithLLM(t *testing.T) {
	s := testService(t)
	requests, mu := conceptLLM(t, s, "巨大的灰色高墙在晨雾里延伸，\n前景一只陶瓷杯，杯身印着“晨光”")
	in := ConceptInput{Prompt: "白色陶瓷杯，杯身印着“晨光”", Form: "mega", Stances: []string{"romanticism"}, Strength: 1, HasReferences: true}
	r, e := s.ComposeConcept(in)
	if e != nil || !r.UsedLLM || len(r.Prompts) != 1 || r.Labels[0] != "巨物 × 浪漫主义" {
		t.Fatal(r, e)
	}
	if !strings.Contains(r.Prompts[0], "\n") {
		t.Fatal("a single stance may keep several lines", r.Prompts[0])
	}
	mu.Lock()
	req := (*requests)[0]
	mu.Unlock()
	form, _ := findConcept(conceptForms, "mega")
	stance, _ := findConcept(conceptStances, "romanticism")
	if !strings.Contains(req, form.Rules) || !strings.Contains(req, stance.Rules) || !strings.Contains(req, conceptRefsNote) || !strings.Contains(req, "白色陶瓷杯") || !strings.Contains(req, conceptStrengths[1]) {
		t.Fatal("request misses the card rules, the reference note or the prompt:", req)
	}
	calls, _ := List[LLMCall](s.store, "llm_call")
	if len(calls) != 1 || calls[0].Stage != conceptStage || calls[0].Status != "completed" {
		t.Fatalf("rewrite not recorded in the LLM monitor: %+v", calls)
	}
	var logged, leaked int
	s.store.db.QueryRow(`SELECT COUNT(*) FROM app_logs WHERE message=?`, "已按概念改写提示词").Scan(&logged)
	s.store.db.QueryRow(`SELECT COUNT(*) FROM app_logs WHERE message LIKE ?`, "%陶瓷杯%").Scan(&leaked)
	if logged != 1 || leaked != 0 {
		t.Fatal("expected one log line without the prompt", logged, leaked)
	}

	// Three stances: three one-line prompts, one request each.
	in.Stances, in.HasReferences = []string{"romanticism", "minimalism", "wabisabi"}, false
	r, e = s.ComposeConcept(in)
	if e != nil || len(r.Prompts) != 3 || r.Labels[2] != "巨物 × 侘寂" {
		t.Fatal(r, e)
	}
	for _, p := range r.Prompts {
		if strings.Contains(p, "\n") {
			t.Fatal("contrast prompts must be one line each", p)
		}
	}
	mu.Lock()
	n := len(*requests)
	last := (*requests)[n-1]
	mu.Unlock()
	if n != 4 || strings.Contains(last, conceptRefsNote) || !strings.Contains(last, "一整行") {
		t.Fatal(n, last)
	}
}

func TestComposeConceptKeepsQuotedText(t *testing.T) {
	s := testService(t)
	conceptLLM(t, s, "灰色巨墙前的一只陶瓷杯")
	r, e := s.ComposeConcept(ConceptInput{Prompt: "陶瓷杯，杯身印着“晨光”", Form: "mega", Stances: []string{"romanticism"}})
	if e == nil || !strings.Contains(e.Error(), "晨光") || len(r.Prompts) != 0 {
		t.Fatal("a rewrite without the in-image text was accepted", r, e)
	}
}

func TestComposeConceptFallsBackWithoutLLM(t *testing.T) {
	s := testService(t)
	r, e := s.ComposeConcept(ConceptInput{Prompt: "白色陶瓷杯", Form: "ruin", Stances: []string{"wabisabi"}})
	if e != nil || r.UsedLLM || len(r.Prompts) != 1 || !strings.HasPrefix(r.Prompts[0], "白色陶瓷杯\n") || !strings.Contains(r.Prompts[0], "废墟 × 侘寂") || !strings.Contains(r.Prompts[0], "苔藓") {
		t.Fatal(r, e)
	}
	r, e = s.ComposeConcept(ConceptInput{Prompt: "白色陶瓷杯\n放在桌上", Form: "ruin", Stances: []string{"wabisabi", "realism"}})
	if e != nil || len(r.Prompts) != 2 || strings.Contains(r.Prompts[0]+r.Prompts[1], "\n") {
		t.Fatal("contrast fallback must be one line each", r, e)
	}
	if r, e = s.ComposeConcept(ConceptInput{Form: "hive", Stances: []string{"constructivism"}}); e != nil || !strings.HasPrefix(r.Prompts[0], "画面概念") {
		t.Fatal("an empty prompt should still get the concept points", r, e)
	}
}

func TestComposeConceptRejectsBadInput(t *testing.T) {
	s := testService(t)
	for _, in := range []ConceptInput{
		{Form: "nope", Stances: []string{"realism"}},
		{Form: "mega"},
		{Form: "mega", Stances: []string{"nope"}},
		{Form: "mega", Stances: []string{"realism", "realism"}},
		{Form: "mega", Stances: []string{"realism", "minimalism", "wabisabi", "futurism"}},
		{Form: "mega", Stances: []string{"realism"}, Strength: 3},
		{Form: "mega", Stances: []string{"realism"}, Strength: -1},
		{Form: "mega", Stances: []string{"realism"}, Prompt: strings.Repeat("长", 20000)},
	} {
		if _, e := s.ComposeConcept(in); e == nil {
			t.Fatal("accepted", in.Form, in.Stances, in.Strength)
		}
	}
}

func TestSubmitKeepsConceptLabels(t *testing.T) {
	s := testService(t)
	c := saveConnection(t, s, "http://127.0.0.1:1")
	in := request(c)
	in.Prompts = []string{"第一条", "第二条"}
	in.ConceptLabels = []string{"巨物 × 浪漫主义", "巨物 × 极简主义"}
	if _, e := s.Submit(in); e != nil {
		t.Fatal(e)
	}
	tasks, _ := List[Task](s.store, "task")
	got := map[string]string{}
	for _, task := range tasks {
		got[task.Prompt] = task.Concept
	}
	if got["第一条"] != "巨物 × 浪漫主义" || got["第二条"] != "巨物 × 极简主义" {
		t.Fatal(got)
	}
	// Labels that no longer match the prompts (edited afterwards) are dropped.
	in.RequestID, in.Prompts = NewID(), []string{"改过的一条"}
	if _, e := s.Submit(in); e != nil {
		t.Fatal(e)
	}
	tasks, _ = List[Task](s.store, "task")
	for _, task := range tasks {
		if task.Prompt == "改过的一条" && task.Concept != "" {
			t.Fatal("mismatched label kept", task.Concept)
		}
	}
}
