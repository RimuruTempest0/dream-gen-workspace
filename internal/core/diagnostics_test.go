package core

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readZip(t *testing.T, path string) map[string][]byte {
	t.Helper()
	z, e := zip.OpenReader(path)
	if e != nil {
		t.Fatal(e)
	}
	defer z.Close()
	out := map[string][]byte{}
	for _, f := range z.File {
		r, _ := f.Open()
		out[f.Name], _ = io.ReadAll(r)
		r.Close()
	}
	return out
}

func TestDiagnosticPackageCarriesNoSecrets(t *testing.T) {
	s := testService(t)
	const key, host, prompt = "sk-diag-secret-7788", "relay.diag-example.test", "PROMPT-MARKER 金毛卫衣"
	c, e := s.SaveConnection(ConnectionInput{Kind: "image", Name: "诊断线路", BaseURL: "https://" + host + "/v1", Model: "img-model", Key: key})
	if e != nil {
		t.Fatal(e)
	}
	failed := Task{ID: NewID(), BatchID: "b", CreatedAt: Now(), UpdatedAt: Now(), Status: "failed", Prompt: prompt, Model: "img-model", Connection: c,
		Error:      "upstream said Bearer " + key + " at https://" + host + "/v1/images?token=abc",
		Diagnostic: GenerationDiagnostic{Stage: "提交", HTTPStatus: 502, UpstreamRequestID: "req-42"}}
	s.store.Put("task", failed.ID, failed)
	s.store.Put("agent_run", "run-1", AgentRun{ID: "run-1", UpdatedAt: Now(), Status: "failed", Stage: "分析", Error: "AI 模型断开", Input: AgentInput{Instruction: prompt}})
	s.store.appendLog("error", "image", failed.ID, "请求 https://"+host+"/v1/images 失败")
	program := t.TempDir()
	os.WriteFile(filepath.Join(program, "version.json"), []byte("\xef\xbb\xbf"+`{"version":"9.9.9","runtime":"130.0"}`), 0o600)
	os.WriteFile(filepath.Join(program, "customer.dat"), []byte("CUSTOMER-DAT-BYTES"), 0o600)

	dest := filepath.Join(t.TempDir(), "diag.zip")
	if e = s.ExportDiagnostics(dest, program); e != nil {
		t.Fatal(e)
	}
	files := readZip(t, dest)
	for _, name := range []string{"README.txt", "summary.json", "logs.txt", "failures.json"} {
		if _, ok := files[name]; !ok {
			t.Fatal("missing", name)
		}
	}
	all := []byte{}
	for _, b := range files {
		all = append(all, b...)
	}
	for _, secret := range []string{key, host, "PROMPT-MARKER", "金毛卫衣", "CUSTOMER-DAT-BYTES", "token=abc", builtinMarker(t), "\x89PNG"} {
		if bytes.Contains(all, []byte(secret)) {
			t.Fatalf("diagnostic package carries %q", secret)
		}
	}
	var sum diagnosticSummary
	if e = json.Unmarshal(files["summary.json"], &sum); e != nil {
		t.Fatal(e)
	}
	if sum.AppVersion != AppVersion || sum.Package["version"] != "9.9.9" || sum.LicenseMode != "developer" || len(sum.ImageLines) != 1 || sum.ImageLines[0].Name != "诊断线路" || !sum.ImageLines[0].HasKey || sum.OS == "" {
		t.Fatalf("summary %+v", sum)
	}
	var failures []diagnosticFailure
	json.Unmarshal(files["failures.json"], &failures)
	if len(failures) != 2 || !strings.Contains(string(files["failures.json"]), "req-42") || !strings.Contains(string(files["failures.json"]), "AI 模型断开") {
		t.Fatalf("failures %s", files["failures.json"])
	}
	if !strings.Contains(string(files["logs.txt"]), "[线路地址]") {
		t.Fatalf("logs %s", files["logs.txt"])
	}
}
