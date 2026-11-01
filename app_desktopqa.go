//go:build desktopqa

package main

import (
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
	a.startup(a.ctx)
	return nil
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
