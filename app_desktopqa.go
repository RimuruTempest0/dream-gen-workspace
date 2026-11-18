//go:build desktopqa

package main

import (
	"encoding/base64"
	"errors"
	"imageworkbench/internal/core"
	"os"
	"path/filepath"
	"strings"
)

// Isolate desktop test files without deleting data or exposing a reset API in shipping builds.
// The fresh data directory starts as on a Chinese Windows.
func (a *App) ResetDesktopQA() error { return a.ResetDesktopQAWithSystemLanguage("zh-CN") }

// ResetDesktopQAWithSystemLanguage is ResetDesktopQA as on a Windows whose UI
// language is tag (for example "ja-JP" or "de-DE"): the first start of the
// fresh data directory picks the interface language from it.
func (a *App) ResetDesktopQAWithSystemLanguage(tag string) error {
	core.SetTestSystemLanguage(tag)
	if filepath.Base(a.configRoot) != "desktop-qa" {
		return errors.New("仅限独立桌面 QA 实例")
	}
	state, e := a.service.Dashboard()
	if e != nil {
		return e
	}
	if state.Active > 0 {
		return errors.New("测试任务仍在执行")
	}
	root := filepath.Join(filepath.Dir(a.service.Root()), "qa-data-"+core.NewID())
	next, e := core.NewService(root)
	if e != nil {
		return e
	}
	next.InheritProductBrowser(a.service)
	a.service.Close()
	a.service = next
	a.service.SetAgentExecutable(filepath.Join(a.configRoot, "agent", "claude-code", "claude.exe"))
	// The QA program folder has no customer.dat: developer mode, program folder known.
	core.ForgetTestLicenseKey()
	if e = a.service.LoadLicense(a.configRoot); e != nil {
		return e
	}
	a.startup(a.ctx)
	return nil
}

// StartTestCustomerExpiring is StartTestCustomer with an expiry (RFC 3339).
func (a *App) StartTestCustomerExpiring(imageBase, llmBase string, trialImages int, expiresAt string) (map[string]string, error) {
	if e := a.ResetDesktopQA(); e != nil {
		return nil, e
	}
	dir := filepath.Join(filepath.Dir(a.service.Root()), "qa-customer-"+core.NewID())
	id, code, e := core.WriteTestCustomerExpiring(dir, imageBase, llmBase, trialImages, expiresAt)
	if e != nil {
		return nil, e
	}
	if e = a.service.LoadLicense(dir); e != nil {
		return nil, e
	}
	return map[string]string{"softwareID": id, "code": code}, nil
}

// WriteTestRenewal writes a renewed customer.dat for the QA customer and returns its path.
func (a *App) WriteTestRenewal(expiresAt string) (string, error) {
	dir := filepath.Join(filepath.Dir(a.service.Root()), "qa-renewal-"+core.NewID())
	return filepath.Join(dir, "customer.dat"), core.WriteTestRenewal(dir, expiresAt)
}

// StartTestCustomer switches the QA instance to a fresh data directory running a
// signed test customer package (trial first), and returns its ID and code.
func (a *App) StartTestCustomer(imageBase, llmBase string, trialImages int) (map[string]string, error) {
	if e := a.ResetDesktopQA(); e != nil {
		return nil, e
	}
	dir := filepath.Join(filepath.Dir(a.service.Root()), "qa-customer-"+core.NewID())
	id, code, e := core.WriteTestCustomer(dir, imageBase, llmBase, trialImages)
	if e != nil {
		return nil, e
	}
	if e = a.service.LoadLicense(dir); e != nil {
		return nil, e
	}
	return map[string]string{"softwareID": id, "code": code}, nil
}

// StartTestHosted switches the QA instance to a fresh data directory running the
// mass-market program in an empty program folder (the open edition), and returns
// a signed licence file kept elsewhere, its activation code and the program folder.
func (a *App) StartTestHosted(imageBase, llmBase string, trialImages int) (map[string]string, error) {
	if e := a.ResetDesktopQA(); e != nil {
		return nil, e
	}
	base := filepath.Join(filepath.Dir(a.service.Root()), "qa-customer-"+core.NewID())
	program := filepath.Join(base, "program")
	if e := os.MkdirAll(program, 0o700); e != nil {
		return nil, e
	}
	id, code, e := core.WriteTestHosted(filepath.Join(base, "licence"), imageBase, llmBase, trialImages)
	if e != nil {
		return nil, e
	}
	if e = a.service.LoadLicense(program); e != nil {
		return nil, e
	}
	return map[string]string{"softwareID": id, "code": code, "file": filepath.Join(base, "licence", "customer.dat"), "program": program}, nil
}

// RemoveTestLicenceFile deletes customer.dat from a QA program folder and reads
// the licence again, as closing the program, deleting the file and opening it does.
func (a *App) RemoveTestLicenceFile(program string) error {
	if !strings.HasPrefix(filepath.Base(filepath.Dir(program)), "qa-customer-") {
		return errors.New("仅限桌面 QA 的程序目录")
	}
	if e := os.Remove(filepath.Join(program, "customer.dat")); e != nil {
		return e
	}
	return a.service.LoadLicense(program)
}

// StartTestUpdate makes the QA instance offer version 9.9.9 from a local mock and
// returns the signed manifest and package (base64) for the mock to serve; base ""
// switches it off again. The tests never install it.
func (a *App) StartTestUpdate(base string) (map[string]string, error) {
	manifest, pkg, e := a.service.PrepareTestUpdate(base)
	if e != nil {
		return nil, e
	}
	return map[string]string{"manifest": base64.StdEncoding.EncodeToString(manifest), "package": base64.StdEncoding.EncodeToString(pkg)}, nil
}
