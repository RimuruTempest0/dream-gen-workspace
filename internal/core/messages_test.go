package core

import (
	"strings"
	"testing"
	"unicode"
)

// Backend messages for generation, tasks and connections follow the interface
// language (0.8.0). Upstream error text stays as the service sent it, and
// stored text keeps the language it was written in (ADR 0004).

func hasHan(s string) bool {
	return strings.ContainsFunc(s, func(r rune) bool { return unicode.Is(unicode.Han, r) })
}

func english(t *testing.T) *Service {
	t.Helper()
	s := testService(t)
	if e := s.SetLanguage("en"); e != nil {
		t.Fatal(e)
	}
	return s
}

func TestTaskAndConnectionErrorsInEnglish(t *testing.T) {
	s := english(t)
	g := &fakeGateway{models: []string{"test-image"}}
	c := gatewayConnection(t, s, "relay", newFakeGateway(t, g).URL+"/v1")
	in := request(c)
	in.Count = 0
	if _, e := s.Submit(in); e == nil || e.Error() != "Images per prompt must be 1–20" {
		t.Fatal(e)
	}
	in = request(c)
	in.Compare = true
	if _, e := s.Submit(in); e == nil || !strings.Contains(e.Error(), "side-by-side") || !strings.Contains(e.Error(), "mood") {
		t.Fatal(e)
	}
	if _, e := s.SaveConnection(ConnectionInput{Name: " ", BaseURL: "https://example.com/v1"}); e == nil || e.Error() != "Enter a connection name" {
		t.Fatal(e)
	}
	if _, e := s.SaveConnection(ConnectionInput{Name: "x", BaseURL: "ftp://example.com"}); e == nil || hasHan(e.Error()) || !strings.Contains(e.Error(), "HTTP/HTTPS") {
		t.Fatal(e)
	}
	if e := s.SetImageConcurrency(0); e == nil || hasHan(e.Error()) {
		t.Fatal(e)
	}
	r, e := s.TestConnection(ConnectionInput{ID: c.ID, BaseURL: c.BaseURL})
	if e != nil || r.Auth != "Model query authenticated" || !strings.HasPrefix(r.Models, "Retrieved 1 models") || hasHan(r.Generation) {
		t.Fatalf("%+v %v", r, e)
	}
	if e := s.SetGatewaySettings(GatewaySettings{BackupIDs: []string{"gone"}}); e == nil || hasHan(e.Error()) {
		t.Fatal(e)
	}
}

func TestCancelAndRetryMessagesInEnglish(t *testing.T) {
	s := english(t)
	g := &fakeGateway{models: []string{"test-image"}}
	c := gatewayConnection(t, s, "relay", newFakeGateway(t, g).URL+"/v1")
	submitN(t, s, c, 1) // not started: stays queued
	queued := tasksWhere(s, func(t Task) bool { return t.Status == "queued" })[0]
	if e := s.CancelTask(queued.ID); e != nil {
		t.Fatal(e)
	}
	canceled, _ := s.GetTask(queued.ID)
	if canceled.Error != "Generation canceled. This image was never sent, so there is no charge." {
		t.Fatal(canceled.Error)
	}
	if e := s.CancelTask(queued.ID); e == nil || e.Error() != "This image has already finished; nothing to cancel" {
		t.Fatal(e)
	}
	if e := s.RetryDownload(queued.ID); e == nil || hasHan(e.Error()) {
		t.Fatal(e)
	}
}

// The relay's own words stay verbatim after the software's explanation, which
// is in the interface language.
func TestRelayErrorKeepsRawTextWithEnglishExplanation(t *testing.T) {
	s := english(t)
	raw := "无效的令牌 Invalid API key provided"
	g := &fakeGateway{models: []string{"test-image"}, refuse: 401, refuseMessage: raw}
	c := gatewayConnection(t, s, "relay", newFakeGateway(t, g).URL+"/v1")
	s.SetImageConcurrency(1)
	submitN(t, s, c, 2)
	s.Start(func() {})
	waitFor(t, "key rejected", func() bool { return countStatus(s, "failed") == 1 && countStatus(s, "blocked") == 1 })
	failed := tasksWhere(s, func(t Task) bool { return t.Status == "failed" })[0]
	explanation, original, ok := strings.Cut(failed.Error, "Original error: ")
	if !ok || !strings.HasPrefix(explanation, "Invalid API key (HTTP 401).") || hasHan(explanation) {
		t.Fatal(failed.Error)
	}
	if original != "HTTP 401："+raw {
		t.Fatalf("the relay's text must stay verbatim: %q", original)
	}
	paused := tasksWhere(s, func(t Task) bool { return t.Status == "blocked" })[0]
	if hasHan(paused.Error) || !strings.Contains(paused.Error, "API key is invalid") {
		t.Fatal(paused.Error)
	}
	// The monitor still tells a refused key apart, with English labels.
	m, _ := s.GatewayMonitor(MonitorQuery{Days: 1})
	if m.FailedAuth != 1 {
		t.Fatalf("monitor: %+v", m)
	}
	for _, call := range m.Calls {
		if hasHan(call.SourceLabel) {
			t.Fatal(call.SourceLabel)
		}
	}
}

// A refusal by the content check is recognised in stored text of any language.
func TestPolicyRefusalRecognisedInEitherLanguage(t *testing.T) {
	s := english(t)
	for _, text := range []string{
		s.msg("generation.policyRefused") + " (HTTP 400)",
		"生图模型的内容审核拒绝了这张图（HTTP 400）",
	} {
		task := Task{Status: "failed", Error: text, Diagnostic: GenerationDiagnostic{StartedAt: Now(), ElapsedMS: 1000, HTTPStatus: 403}}
		if o, counted := taskOutcome(task); counted || o.rejected != "" {
			t.Fatalf("%q counted as a rejected key", text)
		}
	}
}

// Logs keep the language they were written in; new ones use the new language.
func TestLogsKeepTheirLanguageAfterSwitching(t *testing.T) {
	s := testService(t)
	if e := s.SetImageConcurrency(2); e != nil {
		t.Fatal(e)
	}
	if e := s.SetLanguage("en"); e != nil {
		t.Fatal(e)
	}
	if e := s.SetImageConcurrency(3); e != nil {
		t.Fatal(e)
	}
	page, e := s.ListLogs("", "image", "", 0, 50)
	if e != nil {
		t.Fatal(e)
	}
	var messages []string
	for _, l := range page.Items {
		messages = append(messages, l.Message)
	}
	joined := strings.Join(messages, "\n")
	if !strings.Contains(joined, "同时生成张数调整为 2") || !strings.Contains(joined, "Images at once set to 3") {
		t.Fatal(joined)
	}
	if strings.Contains(joined, "Images at once set to 2") || strings.Contains(joined, "同时生成张数调整为 3") {
		t.Fatal("a log line was translated after the switch:", joined)
	}
	// A task's status change is logged in the language of that moment.
	g := &fakeGateway{models: []string{"test-image"}}
	c := gatewayConnection(t, s, "relay", newFakeGateway(t, g).URL+"/v1")
	submitN(t, s, c, 1)
	queued := tasksWhere(s, func(t Task) bool { return t.Status == "queued" })[0]
	if e := s.CancelTask(queued.ID); e != nil {
		t.Fatal(e)
	}
	page, _ = s.ListLogs("", "image", "Image status: canceled", 0, 50)
	if page.Total != 1 || hasHan(page.Items[0].Message) {
		t.Fatalf("%+v", page)
	}
}
