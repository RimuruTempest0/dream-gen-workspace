package packcheck

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"imageworkbench/internal/core"
)

const testKey = "sk-packcheck-fixture-0123456789"

func testData(t *testing.T) string {
	root := filepath.Join(t.TempDir(), "data")
	st, e := core.Open(root)
	if e != nil {
		t.Fatal(e)
	}
	defer st.Close()
	sealed, e := core.Protect([]byte(testKey))
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(root, "secrets", "fixture.bin"), sealed, 0600); e != nil {
		t.Fatal(e)
	}
	conns := []core.Connection{
		{ID: "llm", Kind: "llm", Name: "Fixture LLM supplier", BaseURL: "https://llm.fixture-supplier.test/v1", Model: "m", Credential: "fixture"},
		{ID: "image", Name: "Fixture image relay", BaseURL: "https://relay.fixture-images.test/v1/", Model: "m"},
	}
	for _, c := range conns {
		if e = st.Put("connection", c.ID, c); e != nil {
			t.Fatal(e)
		}
	}
	return root
}

func testZip(t *testing.T, files map[string][]byte) string {
	path := filepath.Join(t.TempDir(), "shipping.zip")
	f, e := os.Create(path)
	if e != nil {
		t.Fatal(e)
	}
	w := zip.NewWriter(f)
	for name, body := range files {
		entry, e := w.Create(name)
		if e != nil {
			t.Fatal(e)
		}
		entry.Write(body)
	}
	if e = w.Close(); e != nil {
		t.Fatal(e)
	}
	f.Close()
	return path
}

func clean() map[string][]byte {
	// Vendor files may mention a common word that is also a connection name.
	return map[string][]byte{"ImageWorkbench.exe": []byte("app"), "runtime/msedge.dll": []byte("rt Fixture LLM supplier"), "agent/claude-code/claude.exe": []byte("agent Fixture image relay"), "使用说明.txt": []byte("help")}
}

func TestCleanPackagePasses(t *testing.T) {
	findings, e := Check(testZip(t, clean()), Options{DataDir: testData(t)})
	if e != nil || len(findings) != 0 {
		t.Fatal(e, findings)
	}
}

func TestConnectionDetailsAndProfileFilesFail(t *testing.T) {
	root := testData(t)
	sealed, _ := os.ReadFile(filepath.Join(root, "secrets", "fixture.bin"))
	cases := map[string]map[string][]byte{
		"LLM connection API key":            {"ImageWorkbench.exe": []byte("x" + testKey + "x")},
		"LLM connection encrypted key":      {"agent/claude-code/claude.exe": sealed},
		"image relay connection address":    {"使用说明.txt": []byte("see relay.fixture-images.test/v1 for help")},
		"image relay connection host":       {"runtime/a.json": utf16le("relay.fixture-images.test")},
		"LLM connection name":               {"version.json": []byte(`{"n":"Fixture LLM supplier"}`)},
		"unexpected file":                   {"data/app.db": []byte("db")},
		"unexpected file in shipping ZIP: ": {"data-location.txt": []byte("D:/x")},
	}
	for want, extra := range cases {
		files := clean()
		for k, v := range extra {
			files[k] = v
		}
		findings, e := Check(testZip(t, files), Options{DataDir: root})
		if e != nil {
			t.Fatal(e)
		}
		joined := strings.Join(findings, "\n")
		if !strings.Contains(joined, want) {
			t.Errorf("%s not reported: %q", want, findings)
		}
		if strings.Contains(joined, testKey) || strings.Contains(joined, "fixture-images") {
			t.Errorf("finding printed a secret value: %q", findings)
		}
	}
}

func TestCustomerPackageAllowsCustomerFileAndRejectsPlainKeys(t *testing.T) {
	files := clean()
	files["customer.dat"] = []byte(`{"format":1,"payload":"sealed"}`)
	if findings, e := Check(testZip(t, files), Options{}); e != nil || len(findings) != 1 || !strings.Contains(findings[0], "customer.dat") {
		t.Fatal("customer.dat must be rejected outside customer mode", findings, e)
	}
	secrets := []Secret{{Kind: "customer image key", Value: "sk-customer-formal-1234"}}
	if findings, e := Check(testZip(t, files), Options{CustomerFile: true, Secrets: secrets}); e != nil || len(findings) != 0 {
		t.Fatal(findings, e)
	}
	files["customer.dat"] = []byte(`{"key":"sk-customer-formal-1234"}`)
	findings, e := Check(testZip(t, files), Options{CustomerFile: true, Secrets: secrets})
	if e != nil || len(findings) != 1 || !strings.Contains(findings[0], "customer image key found in customer.dat") || strings.Contains(findings[0], "sk-") {
		t.Fatal(findings, e)
	}
}
