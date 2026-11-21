package core

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"imageworkbench/internal/license"
)

// A suite (one smart run) as an older version left it: no language, no shop
// rules, tasks without plan slots or request IDs.
func putFeedbackRun(t *testing.T, s *Service, status string) AgentRun {
	t.Helper()
	r := AgentRun{ID: NewID(), CreatedAt: Now(), UpdatedAt: Now(), Status: status, Stage: "生成", Input: AgentInput{Instruction: "10 张刺绣卫衣套图", TargetCount: 2}}
	if e := s.store.Put("agent_run", r.ID, r); e != nil {
		t.Fatal(e)
	}
	return r
}

func TestSuiteFeedbackIsSavedWithTheSuiteAndSurvivesARestart(t *testing.T) {
	s := testService(t)
	run := putFeedbackRun(t, s, "ready")
	empty, e := s.GetSuiteFeedback(run.ID)
	if e != nil || empty.RunID != run.ID || empty.Verdict != "" || empty.UpdatedAt != "" {
		t.Fatalf("a suite without feedback: %+v %v", empty, e)
	}
	note := "我要的是平铺图，实际是模特上身图"
	saved, e := s.SaveSuiteFeedback(SuiteFeedback{RunID: run.ID, Verdict: "partly-usable", Problems: []string{"missing-shot", "too-similar"}, Note: note})
	if e != nil || saved.UpdatedAt == "" {
		t.Fatalf("save: %+v %v", saved, e)
	}
	root := s.Root()
	s.store.Close()
	reopened, e := NewService(root)
	if e != nil {
		t.Fatal(e)
	}
	defer reopened.Dispose()
	got, e := reopened.GetSuiteFeedback(run.ID)
	if e != nil || got.Verdict != "partly-usable" || strings.Join(got.Problems, ",") != "missing-shot,too-similar" || got.Note != note || got.UpdatedAt != saved.UpdatedAt {
		t.Fatalf("after restart: %+v %v", got, e)
	}
	// It can be changed later.
	if _, e = reopened.SaveSuiteFeedback(SuiteFeedback{RunID: run.ID, Verdict: "usable"}); e != nil {
		t.Fatal(e)
	}
	if got, _ = reopened.GetSuiteFeedback(run.ID); got.Verdict != "usable" || len(got.Problems) != 0 || got.Note != "" {
		t.Fatalf("changed: %+v", got)
	}
}

// feedbackZip exports the run's 反馈包 and returns its entries in order.
// The note's limit counts characters, as the panel does: 8000 Chinese
// characters (24000 bytes) are kept, one more is refused.
func TestSuiteFeedbackNoteLimitCountsCharacters(t *testing.T) {
	s := testService(t)
	run := putFeedbackRun(t, s, "ready")
	note := strings.Repeat("袖", 8000)
	saved, e := s.SaveSuiteFeedback(SuiteFeedback{RunID: run.ID, Verdict: "unusable", Note: note})
	if e != nil || saved.Note != note {
		t.Fatalf("an 8000-character Chinese note was refused: %v", e)
	}
	if _, e = s.SaveSuiteFeedback(SuiteFeedback{RunID: run.ID, Verdict: "unusable", Note: note + "口"}); e == nil || !strings.Contains(e.Error(), "8000") {
		t.Fatalf("an 8001-character note: %v", e)
	}
}

func feedbackZip(t *testing.T, s *Service, runID string) ([]string, map[string][]byte) {
	t.Helper()
	dest := filepath.Join(t.TempDir(), "问题反馈-20261121-1030.zip")
	if e := s.ExportFeedbackPackage(runID, dest); e != nil {
		t.Fatal(e)
	}
	z, e := zip.OpenReader(dest)
	if e != nil {
		t.Fatal(e)
	}
	names := []string{}
	for _, f := range z.File {
		names = append(names, f.Name)
	}
	z.Close()
	return names, readZip(t, dest)
}

func TestStoppedAndFailedSuitesExportAFeedbackPackage(t *testing.T) {
	s := testService(t)
	for _, status := range []string{"interrupted", "failed"} {
		run := putFeedbackRun(t, s, status)
		note := "生成中断，我要的是 10 张，实际只出了 3 张"
		if _, e := s.SaveSuiteFeedback(SuiteFeedback{RunID: run.ID, Verdict: "unusable", Problems: []string{"interrupted"}, Note: note}); e != nil {
			t.Fatal(status, e)
		}
		names, files := feedbackZip(t, s, run.ID)
		if len(names) < 2 || names[0] != "反馈.md" || files["run.json"] == nil {
			t.Fatalf("%s: entries %v", status, names)
		}
		md := string(files["反馈.md"])
		// The verdict comes first, with fixed Chinese labels; the note is verbatim.
		verdict := strings.Index(md, "整套不可用")
		if verdict < 0 || verdict > strings.Index(md, "10 张刺绣卫衣套图") || !strings.Contains(md, "生成中断或报错") || !strings.Contains(md, note) || !strings.Contains(md, AppVersion) {
			t.Fatalf("%s: 反馈.md\n%s", status, md)
		}
		var pkg map[string]any
		if e := json.Unmarshal(files["run.json"], &pkg); e != nil {
			t.Fatal(e)
		}
		if pkg["appVersion"] != AppVersion || pkg["exportedAt"] == "" || pkg["language"] != s.Language() {
			t.Fatalf("%s: package facts %v", status, pkg)
		}
		if _, ok := pkg["customerID"]; ok || strings.Contains(md, "顾客 ID") {
			t.Fatalf("%s: an open-edition package names a customer", status)
		}
		r, _ := pkg["run"].(map[string]any)
		if r["status"] != status || r["input"].(map[string]any)["instruction"] != run.Input.Instruction {
			t.Fatalf("%s: run %v", status, r)
		}
	}
}

func TestFeedbackPackageCarriesNoSecretsAndNamesTheCustomer(t *testing.T) {
	isolateLicenseState(t)
	s := testService(t)
	const host, ownKey, oldHost = "relay.feedback-example.test", "sk-own-secret-5566", "legacy-gateway.example.test"
	customer := writeCustomer(t, "https://"+host+"/v1", 5, 1, "line-image-1", nil)
	if e := s.LoadLicense(customer.dir); e != nil {
		t.Fatal(e)
	}
	own, e := s.SaveConnection(ConnectionInput{Kind: "image", Name: "自配线路", BaseURL: "https://" + host + "/own/v1", Model: "img-model", Key: ownKey})
	if e != nil {
		t.Fatal(e)
	}
	line := imageLine(t, s, "image-1")
	// A line deleted since the run keeps its address in the run's snapshot only.
	gone := Connection{ID: "gone", Name: "旧线路", BaseURL: "https://" + oldHost + "/v1", Model: "old-model"}
	run := AgentRun{ID: NewID(), CreatedAt: Now(), UpdatedAt: Now(), Status: "ready", Input: AgentInput{Instruction: "套图要求 PROMPT-MARKER"}, ImageConnection: gone, LLMConnection: Connection{Name: "LLM 线路 1", Model: "m", BaseURL: "https://" + host + "/v1"},
		Skills: []SkillRecord{{Name: "Etsy 套图", Source: "builtin", Version: 3, Content: "SKILL-TEXT-MARKER"}}}
	s.store.Put("agent_run", run.ID, run)
	for i, c := range []Connection{line, own, gone} {
		task := Task{ID: NewID(), BatchID: "b", AgentRunID: run.ID, PlanItemID: "p1", CreatedAt: Now(), UpdatedAt: Now(), Status: "failed", Prompt: "第 1 张提示词", Model: c.Model, Connection: c, References: []string{"ref-b", "ref-a"},
			Error:      "upstream said Bearer line-image-1 / " + ownKey + " at https://" + host + "/v1/images?token=abc via " + oldHost,
			Diagnostic: GenerationDiagnostic{Stage: "提交", RequestURL: "https://" + oldHost + "/v1/images/generations", HTTPStatus: 502 + i, UpstreamRequestID: "req-42"},
			Results:    []Output{{URL: "https://cdn.example.test/a.png?token=cdn-secret"}}}
		s.store.Put("task", task.ID, task)
	}
	if _, e = s.SaveSuiteFeedback(SuiteFeedback{RunID: run.ID, Verdict: "unusable", Note: "Key 是 " + ownKey}); e != nil {
		t.Fatal(e)
	}
	names, files := feedbackZip(t, s, run.ID)
	all := []byte{}
	for _, b := range files {
		all = append(all, b...)
	}
	dat, _ := os.ReadFile(filepath.Join(customer.dir, license.FileName))
	for _, secret := range []string{ownKey, "line-image-1", "line-image-2", "line-llm-1", host, oldHost, "token=abc", "cdn-secret", string(dat[:min(len(dat), 40)]), customer.code} {
		if bytes.Contains(all, []byte(secret)) {
			t.Fatalf("feedback package carries %q", secret)
		}
	}
	for _, name := range names {
		if strings.Contains(name, license.FileName) || strings.Contains(name, "secrets") {
			t.Fatal("feedback package holds", name)
		}
	}
	var pkg feedbackPackage
	if e = json.Unmarshal(files["run.json"], &pkg); e != nil {
		t.Fatal(e)
	}
	if pkg.CustomerID != customer.id || !strings.Contains(string(files["反馈.md"]), customer.id) {
		t.Fatalf("customer ID missing: %q", pkg.CustomerID)
	}
	// What the provider needs stays: line names and models, prompts, reference
	// order, request IDs, skill text.
	if len(pkg.Tasks) != 3 || pkg.Tasks[0].Line == nil || pkg.Tasks[0].Line.Name != "图片线路 1" || pkg.Tasks[0].Model != "test-image" || pkg.Tasks[0].Prompt != "第 1 张提示词" ||
		strings.Join(pkg.Tasks[0].References, ",") != "ref-b,ref-a" || pkg.Tasks[0].Diagnostic.UpstreamRequestID != "req-42" || pkg.Run.Skills[0].Content != "SKILL-TEXT-MARKER" || pkg.Run.ImageLine.Name != "旧线路" {
		t.Fatalf("package lost facts: %s", files["run.json"])
	}
}

func TestFeedbackPackageLeavesWhatAnOldSuiteNeverRecordedMissing(t *testing.T) {
	s := testService(t)
	// Today's shop rules and skills are not the ones the old suite used.
	if _, e := s.SaveShopRule(ShopRule{Name: "P004", Text: "CURRENT-SHOP-RULE", Enabled: true}); e != nil {
		t.Fatal(e)
	}
	run := putFeedbackRun(t, s, "ready")
	old := Task{ID: NewID(), BatchID: "b", AgentRunID: run.ID, CreatedAt: Now(), UpdatedAt: Now(), Status: "succeeded", Prompt: "旧提示词", AssetIDs: []string{}}
	s.store.Put("task", old.ID, old)
	_, files := feedbackZip(t, s, run.ID)
	var pkg struct {
		Run   map[string]any   `json:"run"`
		Tasks []map[string]any `json:"tasks"`
	}
	if e := json.Unmarshal(files["run.json"], &pkg); e != nil {
		t.Fatal(e)
	}
	for _, field := range []string{"language", "shopRules", "skills", "plans", "llmLine", "imageLine"} {
		if _, ok := pkg.Run[field]; ok {
			t.Errorf("run.%s was filled in: %v", field, pkg.Run[field])
		}
	}
	if len(pkg.Tasks) != 1 {
		t.Fatalf("tasks %v", pkg.Tasks)
	}
	for _, field := range []string{"planItemID", "language", "diagnostic", "line", "refinedFrom", "autoRetryOf"} {
		if _, ok := pkg.Tasks[0][field]; ok {
			t.Errorf("task.%s was filled in: %v", field, pkg.Tasks[0][field])
		}
	}
	if bytes.Contains(files["run.json"], []byte("CURRENT-SHOP-RULE")) || bytes.Contains(files["反馈.md"], []byte("CURRENT-SHOP-RULE")) {
		t.Fatal("today's shop rule was put in an old suite's package")
	}
}

func TestFeedbackPackagePreviewListsWhatWillBeSaved(t *testing.T) {
	s := testService(t)
	run := putFeedbackRun(t, s, "ready")
	s.SaveSuiteFeedback(SuiteFeedback{RunID: run.ID, Verdict: "usable"})
	preview, e := s.PreviewFeedbackPackage(run.ID)
	if e != nil {
		t.Fatal(e)
	}
	names, files := feedbackZip(t, s, run.ID)
	if len(preview.Files) != len(names) || preview.Bytes <= 0 {
		t.Fatalf("preview %+v, package %v", preview, names)
	}
	var total int64
	for i, f := range preview.Files {
		if f.Name != names[i] || f.Bytes <= 0 {
			t.Fatalf("preview %+v, package %v", preview, names)
		}
		total += int64(len(files[f.Name]))
	}
	// Sizes are estimates (the export time differs), within a few bytes.
	if d := preview.Bytes - total; d < -64 || d > 64 {
		t.Fatalf("preview says %d bytes, package holds %d", preview.Bytes, total)
	}
	if _, e = s.PreviewFeedbackPackage("missing"); e == nil {
		t.Fatal("previewed a suite that does not exist")
	}
}

func TestSuiteFeedbackNeedsAVerdictAndSaysWhatIsWrong(t *testing.T) {
	s := testService(t)
	run := putFeedbackRun(t, s, "ready")
	for name, v := range map[string]SuiteFeedback{
		"no verdict":                {RunID: run.ID, Problems: []string{"too-similar"}, Note: "太像了"},
		"unknown verdict":           {RunID: run.ID, Verdict: "great"},
		"not usable, nothing said":  {RunID: run.ID, Verdict: "unusable"},
		"partly usable, blank note": {RunID: run.ID, Verdict: "partly-usable", Note: "  \n "},
		"unknown problem":           {RunID: run.ID, Verdict: "unusable", Problems: []string{"ugly"}},
		"no such suite":             {RunID: "missing", Verdict: "usable"},
	} {
		if _, e := s.SaveSuiteFeedback(v); e == nil {
			t.Errorf("%s: saved", name)
		}
	}
	if got, _ := s.GetSuiteFeedback(run.ID); got.Verdict != "" {
		t.Fatalf("a refused save was kept: %+v", got)
	}
	for _, v := range []SuiteFeedback{
		{RunID: run.ID, Verdict: "usable"},
		{RunID: run.ID, Verdict: "unusable", Problems: []string{"other"}},
		{RunID: run.ID, Verdict: "partly-usable", Note: "我要的是挂拍，实际是上身"},
	} {
		if _, e := s.SaveSuiteFeedback(v); e != nil {
			t.Errorf("%+v: %v", v, e)
		}
	}
}
