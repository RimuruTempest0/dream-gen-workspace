package admin

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// The English update notes come from one call to a verified LLM key of the pool
// that no customer holds; the request carries the Chinese notes and the fixed
// terms of the glossary. Mock LLM only: no paid call.
func TestTranslateNotesUsesAFreeVerifiedPoolKey(t *testing.T) {
	var mu sync.Mutex
	var prompts, auth []string
	llm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []struct {
				Content any `json:"content"`
			} `json:"messages"`
		}
		b, _ := io.ReadAll(r.Body)
		json.Unmarshal(b, &body)
		mu.Lock()
		if len(body.Messages) > 0 {
			p, _ := body.Messages[0].Content.(string)
			prompts = append(prompts, p)
		}
		auth = append(auth, r.Header.Get("x-api-key")+r.Header.Get("Authorization"))
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"content": []map[string]string{{"type": "text", "text": "  Mood guide now asks fewer questions.\n- Faster  "}}})
	}))
	defer llm.Close()
	a := open(t)
	if _, e := a.TranslateNotes("心境引导少问一个问题"); e == nil || !strings.Contains(e.Error(), "LLM Key") {
		t.Fatal("translated without a key:", e)
	}
	stock(t, a, 2, llm.URL+"/v1")
	// The keys with a customer are left alone: their quota is the customer's.
	c, _ := a.CreateCustomer("客户 T", 1)
	a.AutoAssign(c.ID)
	held := []string{}
	a.mu.Lock()
	for _, k := range a.d.Keys {
		if k.Kind == "llm" && k.CustomerID != "" {
			v, e := a.secret(k.SecretRef)
			if e != nil || len(v) == 0 {
				t.Fatal("secret", e)
			}
			held = append(held, string(v))
		}
	}
	a.mu.Unlock()
	if len(held) == 0 {
		t.Fatal("the customer holds no LLM key")
	}
	en, e := a.TranslateNotes("心境引导少问一个问题\n- 更快")
	if e != nil {
		t.Fatal(e)
	}
	if en != "Mood guide now asks fewer questions.\n- Faster" {
		t.Fatalf("%q", en)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(prompts) != 1 || !strings.Contains(prompts[0], "心境引导少问一个问题\n- 更快") || !strings.Contains(prompts[0], "心境引导 = mood guide") || !strings.Contains(prompts[0], "into English") {
		t.Fatalf("prompt %q", prompts)
	}
	if !strings.Contains(auth[0], "sk-llm-") {
		t.Fatal("no pool key sent", auth)
	}
	for _, v := range held {
		if strings.Contains(auth[0], v) {
			t.Fatal("translated with a customer's key")
		}
	}
	if _, e := a.TranslateNotes("  "); e == nil {
		t.Fatal("translated empty notes")
	}
}
