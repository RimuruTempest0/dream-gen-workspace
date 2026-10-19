package core

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCapabilityProbeRequiresActualMatchingToolUse(t *testing.T) {
	for _, test := range []struct {
		name  string
		block func(string) messageBlock
	}{
		{"text only", func(n string) messageBlock {
			return messageBlock{Type: "text", Text: "Called capability_echo with nonce " + n}
		}},
		{"wrong nonce", func(n string) messageBlock {
			return messageBlock{Type: "tool_use", Name: "capability_echo", Input: map[string]any{"nonce": "wrong"}}
		}},
		{"wrong tool", func(n string) messageBlock {
			return messageBlock{Type: "tool_use", Name: "other", Input: map[string]any{"nonce": n}}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			s := testService(t)
			up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body map[string]any
				json.NewDecoder(r.Body).Decode(&body)
				if body["stream"] == true {
					writeFakeMessage(w, []messageBlock{{Type: "text", Text: "OK"}}, "")
					return
				}
				block := messageBlock{Type: "text", Text: "OK"}
				if body["tools"] != nil {
					content := body["messages"].([]any)[0].(map[string]any)["content"].(string)
					block = test.block(strings.TrimPrefix(content, "Call capability_echo with nonce "))
					if body["max_tokens"].(float64) != 512 {
						t.Error("probe exceeded configured provider output limit")
					}
				}
				json.NewEncoder(w).Encode(map[string]any{"content": []messageBlock{block}})
			}))
			defer up.Close()
			c := saveLLM(t, s, up.URL)
			c, e := s.SaveConnection(ConnectionInput{ID: c.ID, Kind: "llm", Name: c.Name, BaseURL: c.BaseURL, Model: c.Model, Mode: "compatible", AuthMode: "api-key", MaxOutputTokens: 512})
			if e != nil {
				t.Fatal(e)
			}
			cap, e := s.ValidateLLMCapabilities(c.ID, "invalid-tool")
			if e != nil || cap.Tools || !cap.Text || !cap.Streaming {
				t.Fatal("invalid tool response passed or unrelated capabilities failed", cap, e)
			}
		})
	}
}
