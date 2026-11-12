//go:build desktopqa

package main

import (
	"encoding/base64"
	"errors"
	"imageworkbench/internal/core"
	"path/filepath"
)

// Isolate desktop test files without deleting data or exposing a reset API in shipping builds.
func (a *App) ResetDesktopQA() error {
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
