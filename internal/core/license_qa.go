//go:build desktopqa

package core

import (
	"crypto/ed25519"
	"encoding/base64"
	"os"
	"path/filepath"

	"imageworkbench/internal/license"
)

// WriteTestCustomer writes a signed customer.dat into dir for desktop QA and
// trusts its signing key. Trial and normal use share the lines; the image lines
// use the keys "line-image-1"/"line-image-2" against the same mock, and the
// trial record is kept in a QA-only folder and registry key.
func WriteTestCustomer(dir, imageBase, llmBase string, trialImages int) (string, string, error) {
	pub, priv, e := ed25519.GenerateKey(nil)
	if e != nil {
		return "", "", e
	}
	licensePublicKey = base64.StdEncoding.EncodeToString(pub)
	stateDir := filepath.Join(dir, "license-state")
	licenseStateDir = func() string { return stateDir }
	id, _ := license.NewSoftwareID()
	licenseRegistryKey = `Software\ImageWorkbench\QA\` + id
	code, _ := license.NewCode()
	check, e := license.NewCodeCheck(code, id)
	if e != nil {
		return "", "", e
	}
	conns := []license.Connection{
		{Slot: "image-1", Kind: "image", Name: "图片线路 1", BaseURL: imageBase, Model: "mock-image", Key: "line-image-1"},
		{Slot: "image-2", Kind: "image", Name: "图片线路 2", BaseURL: imageBase, Model: "mock-image", Key: "line-image-2"},
		{Slot: "llm-1", Kind: "llm", Name: "LLM 线路 1", BaseURL: llmBase, Model: "mock-agent", Mode: "compatible", AuthMode: "api-key", Verified: true, Key: "line-llm-1"},
		{Slot: "llm-2", Kind: "llm", Name: "LLM 线路 2", BaseURL: llmBase, Model: "mock-agent", Mode: "compatible", AuthMode: "api-key", Verified: true, Key: "line-llm-2"},
	}
	for i := range conns {
		conns[i].KeyObf, _ = license.ObfuscateKey(conns[i].Key, id)
		conns[i].Key = ""
	}
	data, e := license.Sign(license.Profile{SoftwareID: id, Customer: "QA 客户", IssuedAt: Now(), TrialImages: trialImages, Connections: conns, ImageBackups: []string{"image-2"}, LinesRevision: 1, CodeCheck: check}, priv)
	if e != nil {
		return "", "", e
	}
	if e = os.MkdirAll(dir, 0o700); e != nil {
		return "", "", e
	}
	return id, code, os.WriteFile(filepath.Join(dir, license.FileName), data, 0o600)
}
