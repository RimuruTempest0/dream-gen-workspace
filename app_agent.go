package main

import (
	"encoding/base64"
	"errors"
	"github.com/wailsapp/wails/v2/pkg/runtime"
	"imageworkbench/internal/core"
	"os"
	"path/filepath"
)

func (a *App) AgentRuntime() core.AgentRuntimeInfo { return a.service.AgentRuntime() }
func (a *App) StartAgentAnalysis(v core.AgentInput) (string, error) {
	return a.service.StartAgentAnalysis(v)
}
func (a *App) ContinueAgentAnalysis(v core.AgentContinuation) error {
	return a.service.ContinueAgentAnalysis(v)
}
func (a *App) CancelAgentAnalysis(id string) error { return a.service.CancelAgentAnalysis(id) }
func (a *App) ClearProductBrowserData() error      { return a.service.ClearProductBrowserData() }
func (a *App) ListAgentRuns(offset, limit int) (core.AgentPage, error) {
	return a.service.ListAgentRuns(offset, limit)
}
func (a *App) GetAgentRun(id string) (core.AgentRun, error) { return a.service.GetAgentRun(id) }
func (a *App) GetAgentTasks(id string) ([]core.Task, error) { return a.service.GetAgentTasks(id) }
func (a *App) RetryFailedAgentImages(runID string) (int, error) {
	return a.service.RetryFailedAgentImages(runID)
}
func (a *App) ImageConcurrency() int { return a.service.ImageConcurrency() }
func (a *App) SetImageConcurrency(n int) error {
	return a.service.SetImageConcurrency(n)
}
func (a *App) GetGatewaySettings() (core.GatewaySettings, error) {
	return a.service.GetGatewaySettings()
}
func (a *App) SetGatewaySettings(g core.GatewaySettings) error {
	return a.service.SetGatewaySettings(g)
}
func (a *App) GatewayHealth() ([]core.GatewayHealth, error) { return a.service.GatewayHealth() }
func (a *App) CheckGateways() ([]core.GatewayHealth, error) { return a.service.CheckGateways() }
func (a *App) LicenseStatus() core.LicenseStatus            { return a.service.LicenseStatus() }
func (a *App) Activate(code string) (core.LicenseStatus, error) {
	return a.service.Activate(code)
}
func (a *App) GatewayMonitor(q core.MonitorQuery) (core.GatewayMonitorResult, error) {
	return a.service.GatewayMonitor(q)
}
func (a *App) LLMMonitor(q core.MonitorQuery) (core.LLMMonitorResult, error) {
	return a.service.LLMMonitor(q)
}
func (a *App) ExportGatewayCallsToPath(q core.MonitorQuery, path string) (int, error) {
	return a.service.ExportGatewayCalls(q, path)
}
func (a *App) ExportLLMCallsToPath(q core.MonitorQuery, path string) (int, error) {
	return a.service.ExportLLMCalls(q, path)
}
func (a *App) ListConnectionModels(id string) ([]string, error) {
	return a.service.ListConnectionModels(id)
}
func (a *App) RefineAgentImage(v core.RefineInput) (string, error) {
	return a.service.RefineAgentImage(v)
}
func (a *App) GetAgentDraft() (core.AgentInput, error) { return a.service.GetAgentDraft() }
func (a *App) SetAgentDraft(v core.AgentInput) error   { return a.service.SetAgentDraft(v) }
func (a *App) SaveSuitePlan(id string, v core.SuitePlan) (core.SuitePlan, error) {
	return a.service.SaveSuitePlan(id, v)
}
func (a *App) SubmitSuite(id string, revision int, requestID string) (string, error) {
	return a.service.SubmitSuite(id, revision, requestID)
}
func (a *App) ValidateLLMCapabilities(id, testID string) (core.LLMCapabilities, error) {
	return a.service.ValidateLLMCapabilities(id, testID)
}
func (a *App) ListSkills() ([]core.SkillRecord, error)                { return a.service.ListSkills() }
func (a *App) SaveSkill(v core.SkillRecord) (core.SkillRecord, error) { return a.service.SaveSkill(v) }
func (a *App) SetSkillEnabled(id string, enabled bool) error {
	return a.service.SetSkillEnabled(id, enabled)
}
func (a *App) DeleteSkill(id string) error { return a.service.DeleteSkill(id) }
func (a *App) ImportSkill(path string, replace bool) (core.SkillRecord, error) {
	return a.service.ImportSkill(path, replace)
}
func (a *App) ChooseSkillSource(directory bool) (string, error) {
	if directory {
		return runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{Title: "选择含 SKILL.md 的技能文件夹"})
	}
	return runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{Title: "选择 Skill ZIP", Filters: []runtime.FileFilter{{DisplayName: "Skill ZIP", Pattern: "*.zip"}}})
}
func (a *App) ImportAgentFiles() ([]core.Asset, error) {
	paths, e := runtime.OpenMultipleFilesDialog(a.ctx, runtime.OpenDialogOptions{Title: "添加分析参考图（最多 20 张）", Filters: []runtime.FileFilter{{DisplayName: "图片", Pattern: "*.png;*.jpg;*.jpeg;*.webp"}}})
	if e != nil {
		return nil, e
	}
	if len(paths) > 20 {
		return nil, errors.New("分析参考图最多 20 张")
	}
	out := []core.Asset{}
	for _, path := range paths {
		info, e := os.Stat(path)
		if e != nil {
			return out, e
		}
		if info.Size() > core.MaxImageBytes {
			return out, errors.New("图片超过 32 MB")
		}
		data, e := os.ReadFile(path)
		if e != nil {
			return out, e
		}
		asset, e := a.service.ImportImage(filepath.Base(path), base64.StdEncoding.EncodeToString(data))
		if e != nil {
			return out, e
		}
		out = append(out, asset)
	}
	return out, nil
}

func (a *App) StartSmartGeneration(v core.AgentInput, requestID string) (string, error) {
	return a.service.StartSmartGeneration(v, requestID)
}
func (a *App) ContinueSmartGeneration(v core.AgentContinuation, requestID string) (string, error) {
	return a.service.ContinueSmartGeneration(v, requestID)
}

func (a *App) CurrentSmartProgress() (core.SmartProgress, error) {
	return a.service.CurrentSmartProgress()
}
