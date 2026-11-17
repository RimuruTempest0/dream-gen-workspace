package core

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// thinkingRelay is a Messages endpoint behind a relay that drops a connection
// on which nothing was sent for a while (幻灵, 11/17: a non-streaming sample
// check got "unexpected EOF" after 60 s, the next attempt hit the 3-minute
// deadline). A model that thinks before answering sends nothing for minutes
// unless the reply is streamed, so only a streamed request gets an answer.
func thinkingRelay(t *testing.T, answer string) (*httptest.Server, *[]bool) {
	t.Helper()
	streamed := []bool{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Stream bool `json:"stream"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		streamed = append(streamed, body.Stream)
		if !body.Stream {
			conn, _, e := w.(http.Hijacker).Hijack()
			if e == nil {
				conn.Close() // idle too long: closed without an answer
			}
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		event := func(kind string, v any) {
			b, _ := json.Marshal(v)
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", kind, b)
			w.(http.Flusher).Flush()
		}
		event("message_start", map[string]any{"type": "message_start", "message": map[string]any{"usage": map[string]any{"input_tokens": 10}}})
		event("content_block_start", map[string]any{"type": "content_block_start", "index": 0, "content_block": map[string]any{"type": "thinking", "thinking": ""}})
		event("content_block_delta", map[string]any{"type": "content_block_delta", "index": 0, "delta": map[string]any{"type": "thinking_delta", "thinking": "先看字母，再看头像……"}})
		event("content_block_stop", map[string]any{"type": "content_block_stop", "index": 0})
		event("content_block_start", map[string]any{"type": "content_block_start", "index": 1, "content_block": map[string]any{"type": "text", "text": ""}})
		runes := []rune(answer)
		for _, part := range []string{string(runes[:len(runes)/2]), string(runes[len(runes)/2:])} {
			event("content_block_delta", map[string]any{"type": "content_block_delta", "index": 1, "delta": map[string]any{"type": "text_delta", "text": part}})
		}
		event("content_block_stop", map[string]any{"type": "content_block_stop", "index": 1})
		event("message_delta", map[string]any{"type": "message_delta", "delta": map[string]any{"stop_reason": "end_turn"}, "usage": map[string]any{"output_tokens": 30}})
		event("message_stop", map[string]any{"type": "message_stop"})
	}))
	t.Cleanup(server.Close)
	return server, &streamed
}

// The sample check streams its request, so a thinking model behind a relay that
// drops idle connections still answers.
func TestSampleCheckStreamsSoAThinkingModelIsNotCutOff(t *testing.T) {
	s := testService(t)
	relay, streamed := thinkingRelay(t, `{"pass": false, "problems": ["头饰画成了圣诞帽，应是迷你圣诞袜"]}`)
	c := normalizeConnection(Connection{Kind: "llm", BaseURL: relay.URL, Model: "deepseek-flash", Mode: "compatible", AuthMode: "bearer"})
	pass, problems, e := s.askSampleCheck(context.Background(), c, "k", Task{Prompt: "头饰是迷你圣诞袜"}, [][]byte{tinyPNG(), tinyPNG()})
	if e != nil {
		t.Fatalf("check not finished: %v (streamed: %v)", e, *streamed)
	}
	if pass || strings.Join(problems, "") != "头饰画成了圣诞帽，应是迷你圣诞袜" {
		t.Fatalf("verdict not read from the stream: %v %v", pass, problems)
	}
	calls, _ := List[LLMCall](s.store, "llm_call")
	if len(calls) != 1 || !calls[0].Stream || calls[0].Status != "completed" || calls[0].OutputTokens != 30 {
		t.Fatalf("streamed check not recorded: %+v", calls)
	}
}
