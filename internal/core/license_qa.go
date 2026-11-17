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
	return WriteTestCustomerExpiring(dir, imageBase, llmBase, trialImages, "")
}

// qaCustomer remembers the last QA package so a renewal can be signed for it.
var qaCustomer struct {
	priv                         ed25519.PrivateKey
	id, code, imageBase, llmBase string
	trial                        int
}

// WriteTestCustomerExpiring is WriteTestCustomer with an expiry (RFC 3339, "" = none).
func WriteTestCustomerExpiring(dir, imageBase, llmBase string, trialImages int, expiresAt string) (string, string, error) {
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
	qaCustomer.priv, qaCustomer.id, qaCustomer.code, qaCustomer.imageBase, qaCustomer.llmBase, qaCustomer.trial = priv, id, code, imageBase, llmBase, trialImages
	return id, code, writeQACustomer(dir, expiresAt)
}

// WriteTestRenewal signs a renewed customer.dat for the last QA package into dir.
func WriteTestRenewal(dir, expiresAt string) error {
	return writeQACustomer(dir, expiresAt)
}

func writeQACustomer(dir, expiresAt string) error {
	q := qaCustomer
	id, code, priv, imageBase, llmBase, trialImages := q.id, q.code, q.priv, q.imageBase, q.llmBase, q.trial
	check, e := license.NewCodeCheck(code, id)
	if e != nil {
		return e
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
	data, e := license.Sign(license.Profile{SoftwareID: id, Customer: "QA 客户", IssuedAt: Now(), TrialImages: trialImages, Connections: conns, ImageBackups: []string{"image-2"}, LinesRevision: 1, CodeCheck: check, ExpiresAt: expiresAt}, priv)
	if e != nil {
		return e
	}
	if e = os.MkdirAll(dir, 0o700); e != nil {
		return e
	}
	return os.WriteFile(filepath.Join(dir, license.FileName), data, 0o600)
}

// ForgetTestLicenseKey returns the QA instance to a developer build (no trusted
// key), as after a reset; customer packages set one again.
func ForgetTestLicenseKey() { licensePublicKey, productLine, updatePublicKey = "", "", "" }

// WriteTestHosted writes a signed customer.dat into dir and makes this QA process
// a mass-market program that trusts its key: without the file beside the program
// it is the open edition, and importing the file makes it the hosted one.
func WriteTestHosted(dir, imageBase, llmBase string, trialImages int) (string, string, error) {
	id, code, e := WriteTestCustomerExpiring(dir, imageBase, llmBase, trialImages, "")
	productLine, updatePublicKey, licensePublicKey = "creative", licensePublicKey, ""
	return id, code, e
}
