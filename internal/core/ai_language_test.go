package core

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// 对话语言与生图提示词 (0.8.0): what the requests to the AI 模型 ask for.

const (
	wantChinesePrompt = "生图提示词用中文写"
	wantEnglishPrompt = "生图提示词用英文（English）写"
)

// replyNames is how each language is named in a request's 对话语言 line.
var replyNames = map[string]string{"zh-CN": "一律用中文写", "en": "一律用英文（English）写", "vi": "一律用越南语（Tiếng Việt）写", "ja": "一律用日语（日本語）写", "ko": "一律用韩语（한국어）写"}

// languageLLM is a local mock AI 模型 that records each request's text. Before
// answering request n, before(n) may act; it returns true to drop the connection
// without an answer (asked again: nothing was sent to the image service).
func languageLLM(t *testing.T, s *Service, reply string, before func(n int) bool) (func() []string, *httptest.Server) {
	t.Helper()
	var mu sync.Mutex
	requests := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []struct {
				Content json.RawMessage `json:"content"`
			} `json:"messages"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		text := ""
		if json.Unmarshal(body.Messages[0].Content, &text) != nil {
			var blocks []struct {
				Type, Text string
			}
			json.Unmarshal(body.Messages[0].Content, &blocks)
			for _, b := range blocks {
				text += b.Text
			}
		}
		mu.Lock()
		requests = append(requests, text)
		n := len(requests)
		mu.Unlock()
		if before != nil && before(n) {
			if conn, _, e := w.(http.Hijacker).Hijack(); e == nil {
				conn.Close()
			}
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"content": []map[string]any{{"type": "text", "text": reply}}, "usage": map[string]any{"input_tokens": 300, "output_tokens": 80}})
	}))
	t.Cleanup(server.Close)
	if _, e := s.SaveConnection(ConnectionInput{Kind: "llm", Name: "LLM 线路", BaseURL: server.URL, Model: "test-llm", Key: "llm-key"}); e != nil {
		t.Fatal(e)
	}
	return func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string{}, requests...)
	}, server
}

func setLanguage(t *testing.T, s *Service, lang string) {
	t.Helper()
	if e := s.SetLanguage(lang); e != nil {
		t.Fatal(e)
	}
}

// A mood rewrite asks for a Chinese 生图提示词 in a Chinese interface and for an
// English one in the other four.
func TestMoodRewriteAsksForThePromptLanguageOfTheInterface(t *testing.T) {
	for lang, want := range map[string]string{"zh-CN": wantChinesePrompt, "en": wantEnglishPrompt, "vi": wantEnglishPrompt, "ja": wantEnglishPrompt, "ko": wantEnglishPrompt} {
		t.Run(lang, func(t *testing.T) {
			s := testService(t)
			up, _, _ := moodRelay(t)
			requests, _ := languageLLM(t, s, "A white mug printed with “晨光” in drizzle at night", nil)
			setLanguage(t, s, lang)
			if _, e := s.Submit(moodInput(saveConnection(t, s, up.URL))); e != nil {
				t.Fatal(e)
			}
			waitReworded(t, s)
			reqs := requests()
			if len(reqs) != 1 || !strings.Contains(reqs[0], want) {
				t.Fatalf("request does not ask for the prompt language %q:\n%v", want, reqs)
			}
			other := wantEnglishPrompt
			if want == wantEnglishPrompt {
				other = wantChinesePrompt
			}
			if strings.Contains(reqs[0], other) {
				t.Fatal("request asks for both languages")
			}
		})
	}
}

// The concept stage asks for the prompt language too.
func TestConceptAsksForThePromptLanguageOfTheInterface(t *testing.T) {
	for lang, want := range map[string]string{"zh-CN": wantChinesePrompt, "ja": wantEnglishPrompt} {
		s := testService(t)
		requests, _ := languageLLM(t, s, "A huge grey wall in morning fog; a mug printed with “晨光”", nil)
		setLanguage(t, s, lang)
		if _, e := s.ComposeConcept(ConceptInput{Prompt: "白色陶瓷杯，杯身印着“晨光”", Form: "mega", Stances: []string{"romanticism", "minimalism"}, Strength: 1}); e != nil {
			t.Fatal(lang, e)
		}
		for _, req := range requests() {
			if !strings.Contains(req, want) {
				t.Fatalf("%s: request does not ask for %q", lang, want)
			}
		}
	}
}

// The planning Agent talks to the customer in the 对话语言 and writes its
// prompts in the prompt language: English for English, Vietnamese, Japanese and
// Korean, Chinese for Chinese.
func TestAgentUsesTheConversationAndPromptLanguage(t *testing.T) {
	for lang, prompt := range map[string]string{"zh-CN": "prompt 是生图提示词，用中文写", "en": "prompt 是生图提示词，用英文（English）写", "vi": "prompt 是生图提示词，用英文（English）写", "ja": "prompt 是生图提示词，用英文（English）写", "ko": "prompt 是生图提示词，用英文（English）写"} {
		t.Run(lang, func(t *testing.T) {
			s := testService(t)
			llm := saveLLM(t, s, "https://example.com/anthropic")
			images := saveConnection(t, s, "https://example.com/v1")
			var got string
			s.agentRunner = func(ctx context.Context, in agentInvocation) error {
				got = in.Prompt
				return errors.New("stop")
			}
			setLanguage(t, s, lang)
			if _, e := s.StartSmartGeneration(agentInput(llm, images), "lang-"+lang); e != nil {
				t.Fatal(e)
			}
			s.waitAgent()
			if !strings.Contains(got, replyNames[lang]) || !strings.Contains(got, prompt) {
				t.Fatalf("agent prompt misses the language rules (%s / %s):\n%s", replyNames[lang], prompt, got[:min(len(got), 600)])
			}
			if strings.Contains(got, "用中文沟通") || strings.Contains(got, "用中文简短报告") {
				t.Fatal("agent prompt still fixes Chinese")
			}
		})
	}
}

// The sample check's problems are shown to the customer: they come in the
// 对话语言 the images were submitted in.
func TestSampleCheckAsksForTheConversationLanguage(t *testing.T) {
	s := testService(t)
	requests, server := languageLLM(t, s, `{"pass": true, "problems": []}`, nil)
	c := normalizeConnection(Connection{Kind: "llm", BaseURL: server.URL, Model: "m"})
	if _, _, e := s.askSampleCheck(context.Background(), c, "k", Task{Prompt: "头饰是迷你圣诞袜", Language: "ko"}, [][]byte{tinyPNG()}); e != nil {
		t.Fatal(e)
	}
	if reqs := requests(); len(reqs) != 1 || !strings.Contains(reqs[0], replyNames["ko"]) || strings.Contains(reqs[0], "一句中文") {
		t.Fatalf("sample check does not ask for Korean: %v", reqs)
	}
}

// A switch while a mood image is being written does not change that image: the
// request asked again after a dropped connection keeps the starting language,
// and the next image uses the new one.
func TestSwitchingLanguageMidwayKeepsTheStartingLanguage(t *testing.T) {
	s := testService(t)
	up, calls, mu := moodRelay(t)
	requests, _ := languageLLM(t, s, "A white mug printed with “晨光”", func(n int) bool {
		if n == 1 {
			// The customer switches to Chinese while the rewrite is under way.
			if e := s.SetLanguage("zh-CN"); e != nil {
				t.Error(e)
			}
			return true
		}
		return false
	})
	c := saveConnection(t, s, up.URL)
	setLanguage(t, s, "en")
	if _, e := s.Submit(moodInput(c)); e != nil {
		t.Fatal(e)
	}
	tasks := waitReworded(t, s)
	reqs := requests()
	if len(reqs) != 2 || !strings.Contains(reqs[0], wantEnglishPrompt) || !strings.Contains(reqs[1], wantEnglishPrompt) {
		t.Fatalf("the started rewrite did not keep English: %v", reqs)
	}
	if tasks[0].Language != "en" || tasks[0].Status != "queued" || tasks[0].Prompt != "A white mug printed with “晨光”" {
		t.Fatalf("%+v", tasks[0])
	}
	s.stepNext()
	mu.Lock()
	sent := len(*calls)
	mu.Unlock()
	if sent != 1 {
		t.Fatal("the image was not sent once", sent)
	}
	// The next image starts in the language chosen since.
	if _, e := s.Submit(moodInput(c)); e != nil {
		t.Fatal(e)
	}
	waitReworded(t, s)
	if reqs = requests(); len(reqs) != 3 || !strings.Contains(reqs[2], wantChinesePrompt) {
		t.Fatalf("the next rewrite is not in the new language: %v", reqs[len(reqs)-1])
	}
}

// An automatic run continued by itself is the same round: it keeps the language
// it began in. A round the customer starts afterwards uses the new one.
func TestAgentRoundKeepsItsLanguageWhenContinuedByItself(t *testing.T) {
	s := testService(t)
	llm := saveLLM(t, s, "https://example.com/anthropic")
	images := saveConnection(t, s, "https://example.com/v1")
	var mu sync.Mutex
	prompts := []string{}
	s.agentRunner = func(ctx context.Context, in agentInvocation) error {
		mu.Lock()
		prompts = append(prompts, in.Prompt)
		first := len(prompts) == 1
		mu.Unlock()
		if first {
			if e := s.SetLanguage("zh-CN"); e != nil {
				return e
			}
		}
		return errors.New("no plan this time")
	}
	setLanguage(t, s, "ja")
	in := agentInput(llm, images)
	in.AutoMode = true
	if _, e := s.StartSmartGeneration(in, "auto-ja"); e != nil {
		t.Fatal(e)
	}
	s.waitAgent()
	mu.Lock()
	got := append([]string{}, prompts...)
	mu.Unlock()
	if len(got) < 2 {
		t.Fatal("the automatic run did not continue by itself", len(got))
	}
	for _, p := range got {
		if !strings.Contains(p, replyNames["ja"]) {
			t.Fatal("a continuation of the round changed language")
		}
	}
	if _, e := s.StartSmartGeneration(agentInput(llm, images), "next-zh"); e != nil {
		t.Fatal(e)
	}
	s.waitAgent()
	mu.Lock()
	last := prompts[len(prompts)-1]
	mu.Unlock()
	if !strings.Contains(last, replyNames["zh-CN"]) || !strings.Contains(last, "prompt 是生图提示词，用中文写") {
		t.Fatal("the next round is not in the new language")
	}
}

// In-image text is not translated: with an English interface the request says
// so, and the names and slogans the customer wrote reach the image service as
// written. A rewrite that translated them is not sent.
func TestInImageTextIsNotTranslated(t *testing.T) {
	s := testService(t)
	up, calls, mu := moodRelay(t)
	requests, _ := languageLLM(t, s, "A white T-shirt in light drizzle, printed with the name “小明” and the slogan “加油！”; no other text", nil)
	setLanguage(t, s, "en")
	in := moodInput(saveConnection(t, s, up.URL))
	in.Prompts = []string{"一件白色T恤，印着名字“小明”和标语“加油！”"}
	if _, e := s.Submit(in); e != nil {
		t.Fatal(e)
	}
	waitReworded(t, s)
	s.stepNext()
	req := requests()[0]
	if !strings.Contains(req, wantEnglishPrompt) || !strings.Contains(req, "不翻译") || !strings.Contains(req, "“小明”") || !strings.Contains(req, "“加油！”") {
		t.Fatalf("request does not keep the in-image text: %s", req)
	}
	mu.Lock()
	sent := append([]moodCall{}, (*calls)...)
	mu.Unlock()
	if len(sent) != 1 || !strings.Contains(sent[0].prompt, "“小明”") || !strings.Contains(sent[0].prompt, "“加油！”") {
		t.Fatalf("in-image text did not reach the image service as written: %+v", sent)
	}

	// A rewrite that translated the name is held back.
	s2 := testService(t)
	up2, calls2, mu2 := moodRelay(t)
	languageLLM(t, s2, "A white T-shirt printed with “Xiao Ming” and “Go for it!”", nil)
	setLanguage(t, s2, "ko")
	in2 := moodInput(saveConnection(t, s2, up2.URL))
	in2.Prompts = in.Prompts
	if _, e := s2.Submit(in2); e != nil {
		t.Fatal(e)
	}
	tasks := waitReworded(t, s2)
	s2.stepNext()
	mu2.Lock()
	defer mu2.Unlock()
	if tasks[0].Status != "failed" || len(*calls2) != 0 {
		t.Fatalf("a translated in-image text was sent: %+v", tasks[0])
	}
}

// A content-check rewrite keeps the language the image was requested in.
func TestPolicyRewordUsesTheTaskLanguage(t *testing.T) {
	s := testService(t)
	requests, server := languageLLM(t, s, "A mug printed with “晨光”", nil)
	c := normalizeConnection(Connection{Kind: "llm", BaseURL: server.URL, Model: "m"})
	setLanguage(t, s, "zh-CN")
	if _, e := s.rewordPrompt(context.Background(), c, "k", "vi", "一只杯子，印着“晨光”"); e != nil {
		t.Fatal(e)
	}
	if reqs := requests(); len(reqs) != 1 || !strings.Contains(reqs[0], wantEnglishPrompt) {
		t.Fatalf("reword does not follow the task language: %v", reqs)
	}
}

// 再靠近一点 needs no AI 模型: its fixed wording follows the prompt language,
// and a nudge of a nudge does not nest the original prompt.
func TestNudgePromptFollowsThePromptLanguage(t *testing.T) {
	quieter, _ := findMood(moodNudges, "quieter")
	en := nudgePromptIn("ja", "A mug printed with “晨光”", quieter)
	if !strings.HasPrefix(en, "Edit the first image") || !strings.Contains(en, "“晨光”") || strings.Contains(en, "以提供的") {
		t.Fatal(en)
	}
	again := nudgePromptIn("en", en, quieter)
	if strings.Count(again, nudgePromptMarkerEn) != 1 {
		t.Fatal("nested", again)
	}
	if zh := nudgePromptIn("zh-CN", en, quieter); !strings.Contains(zh, quieter.Rules) || strings.Contains(zh, nudgePromptMarkerEn) {
		t.Fatal(zh)
	}
}
