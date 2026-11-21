package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"imageworkbench/internal/i18n"
	"imageworkbench/internal/update"
)

// The rest of the backend messages (#8): smart generation, the planning Agent,
// mood and concept, licence, one-click update, backup, skills and the fixed
// lines added to image prompts follow the interface language.

func TestSmartGenerationAndCreationErrorsInEnglish(t *testing.T) {
	s := english(t)
	if _, e := s.StartAgentAnalysis(AgentInput{}); e == nil || e.Error() != "Enter the request, at most 32000 bytes" {
		t.Fatal(e)
	}
	if e := s.SetSmartMode("nope"); e == nil || e.Error() != "Invalid creation mode" {
		t.Fatal(e)
	}
	if _, e := s.RefineAgentImage(RefineInput{RequestID: "r1"}); e == nil || e.Error() != "Describe how to change this image" {
		t.Fatal(e)
	}
	if _, e := s.NudgeMoodImage(NudgeInput{RequestID: "r1", Direction: "nowhere"}); e == nil || e.Error() != "Choose a direction" {
		t.Fatal(e)
	}
	if _, e := s.ComposeConcept(ConceptInput{}); e == nil || e.Error() != "Choose a form" {
		t.Fatal(e)
	}
	if _, e := resolveMood(MoodChoice{Weather: "nope"}); s.errText(e) != "Invalid mood option. Choose the mood again" {
		t.Fatal(e)
	}
	if info := s.AgentRuntime(); hasHan(info.Message) || info.Message == "" {
		t.Fatal(info.Message)
	}
	if e := s.validatePlan(&AgentRun{}, &SuitePlan{}); e == nil || e.Error() != "A plan must have 1–200 images" {
		t.Fatal(e)
	}
}

func TestLicenceMessagesFollowTheCurrentLanguage(t *testing.T) {
	s := english(t)
	s.mu.Lock()
	s.lic = licenseState{mode: "invalid", message: missingCustomerFile}
	s.mu.Unlock()
	if m := s.LicenseStatus().Message; !strings.HasPrefix(m, "The license file customer.dat is missing") {
		t.Fatal(m)
	}
	if _, e := s.Activate("x"); e == nil || hasHan(e.Error()) {
		t.Fatal(e)
	}
	// The status is not stored text: it shows in the language chosen now.
	setLanguage(t, s, i18n.ZhCN)
	if m := s.LicenseStatus().Message; !strings.HasPrefix(m, "缺少软件授权文件") {
		t.Fatal(m)
	}
	setLanguage(t, s, i18n.Ja)
	if m := s.text(trialUsedUp); !strings.Contains(m, "アクティベーションコード") {
		t.Fatal(m)
	}
}

func TestUpdateAndBackupMessagesInEnglish(t *testing.T) {
	s := english(t)
	if _, e := s.CheckForUpdate(); e == nil || hasHan(e.Error()) || !strings.Contains(e.Error(), "full package") {
		t.Fatal(e)
	}
	ok, why := update.Manifest{Version: "9.0.0", MinVersion: "8.0.0"}.Applies("0.8.0")
	if ok || why == nil || s.errText(why) != "The new version 9.0.0 cannot be installed over this version (it needs 8.0.0 or later). Ask your software provider for the full package" {
		t.Fatal(ok, why)
	}
	if !strings.Contains(why.Error(), "不能从当前版本直接更新") {
		t.Fatal("outside the app (admin tool, logs) a catalog error reads in Chinese:", why)
	}
	bad := filepath.Join(t.TempDir(), "backup.zip")
	os.WriteFile(bad, []byte("not a zip"), 0o600)
	if e := s.RestoreBackup(bad); e == nil || e.Error() != "The backup is not a valid ZIP file" {
		t.Fatal(e)
	}
	if got := UpdateCommandLanguage([]string{"x", updateFlag, "-pid", "1", "-lang", "vi"}); got != i18n.Vi {
		t.Fatal(got)
	}
	if got := UpdateCommandLanguage([]string{"x", updateFlag, "-pid", "1"}); got != i18n.ZhCN {
		t.Fatal("an updater started by a version before 0.8.0 speaks Chinese:", got)
	}
}

func TestSkillImportConflictIsAFlag(t *testing.T) {
	s := english(t)
	source := t.TempDir()
	os.WriteFile(filepath.Join(source, "SKILL.md"), []byte("---\nname: color-rules\ndescription: rules\n---\nbody"), 0o600)
	if _, e := s.ImportSkill(source, false); e != nil {
		t.Fatal(e)
	}
	again, e := s.ImportSkill(source, false)
	if e != nil || !again.Conflict {
		t.Fatal("a second import of the same name asks before replacing", again, e)
	}
	if _, e := s.ImportSkill(filepath.Join(source, "missing"), false); e == nil || hasHan(e.Error()) {
		t.Fatal(e)
	}
	os.WriteFile(filepath.Join(source, "SKILL.md"), []byte("no header"), 0o600)
	if _, e := s.ImportSkill(source, true); e == nil || e.Error() != "SKILL.md needs a YAML header with name and description" {
		t.Fatal(e)
	}
}

func TestHiddenMarkerFollowsTheLanguage(t *testing.T) {
	s := english(t)
	if got := s.redact("key sk-secret failed", "sk-secret"); got != "key [hidden] failed" {
		t.Fatal(got)
	}
	if got := redactLogText("Authorization: abc https://x.example/p?token=1", nil, i18n.En); hasHan(got) || strings.Contains(got, "abc") || strings.Contains(got, "token=1") {
		t.Fatal(got)
	}
	if got := redactIn(i18n.ZhCN, "sk-1", "sk-1"); got != "[已隐藏]" {
		t.Fatal(got)
	}
}

func TestRefusalIsACodeInEveryLanguage(t *testing.T) {
	for _, lang := range i18n.Languages {
		for want, text := range map[string]string{
			"policy":  i18n.T(lang, "generation.policyRefused"),
			"balance": i18n.T(lang, "generation.balance", "status", 402, "raw", "x"),
			"auth":    i18n.T(lang, "generation.keyRejected", "status", 401, "raw", "bad key"),
		} {
			b, _ := json.Marshal(Task{Status: "failed", Error: text})
			var got struct {
				Refusal string `json:"refusal"`
			}
			json.Unmarshal(b, &got)
			if got.Refusal != want {
				t.Errorf("%s %s: %s", lang, want, b)
			}
		}
	}
	if refusalOf(Task{Status: "failed", Error: "API Key 无效（HTTP 401）：insufficient balance"}) != "balance" {
		t.Error("an empty account reported as an invalid key before 0.5.5")
	}
	// 线路额度已用完 covers a used-up trial allowance, and the wording tasks were
	// saved with before it.
	for _, text := range []string{
		i18n.T(i18n.En, "generation.trialQuota", "status", 403, "raw", "x"),
		"The image service account has run out of balance (HTTP 402). The image service refused the request; nothing was made or charged.",
		"图片服务账户余额不足（HTTP 402），图片服务拒绝了请求，未生成也不计费。",
	} {
		if got := refusalOf(Task{Status: "failed", Error: text}); got != "balance" {
			t.Error("not a quota refusal:", text, got)
		}
	}
}

// The fixed lines the backend adds to a 生图提示词 are part of the prompt the
// customer reads: English for a non-Chinese interface, Chinese for Chinese.
func TestFixedPromptLinesFollowThePromptLanguage(t *testing.T) {
	no := false
	r := AgentRun{Language: i18n.Ko, Input: AgentInput{Ratio: "1:1"}, Skills: []SkillRecord{{ID: p004SkillID, Content: shippedP004Contents()[0]}}}
	p := SuitePlan{Locked: []string{"red hoodie"}, Editable: []string{"background"}, CuffEmbroidery: &no}
	got := itemPrompt(r, p, SuiteItem{Prompt: "A hoodie on a hanger", Sample: true, SampleArea: "chest"})
	if hasHan(got) || !strings.Contains(got, "Locked for the whole set: red hoodie") || !strings.Contains(got, "Project fixed rules (P004)") || !strings.HasPrefix(got, "Design-area sample") {
		t.Fatal(got)
	}
	r.Language = i18n.ZhCN
	if got := itemPrompt(r, p, SuiteItem{Prompt: "卫衣"}); !strings.Contains(got, "统一锁定项：red hoodie") {
		t.Fatal(got)
	}
	if got := samplesPromptLine([]string{"chest", ""}, 3, i18n.En) + samplePromptLine(2, i18n.Vi); hasHan(got) || !hasSampleLead(got) {
		t.Fatal(got)
	}
	en := refinePrompt("Samples: reference image 1 is the sample\nA hoodie", "make the text level", 1, i18n.Ja)
	if hasHan(en) || !strings.Contains(en, "make the text level") || baseTaskPrompt(Task{RefinedFrom: "x", Prompt: en}) != "A hoodie" {
		t.Fatal(en)
	}
	if cutSampleFix("prompt"+sampleFixMarkerIn(i18n.En)+"wrong hat") != "prompt" || cutSampleFix("prompt"+sampleFixMarkerIn(i18n.ZhCN)+"帽子") != "prompt" {
		t.Fatal("redraw corrections are dropped in either language")
	}
}

// #5's per-language lists reach #9's seam: the task's labels and the no-AI
// fallback use the language the task was started in.
func TestMoodTaskUsesItsCapturedLanguage(t *testing.T) {
	s := english(t)
	m, e := resolveMood(MoodChoice{"drizzle", "night", "far", "soothe"})
	if e != nil {
		t.Fatal(e)
	}
	concept, _ := resolveConceptChoice(&ConceptChoice{Form: conceptForms[0].ID, Stance: conceptStances[0].ID})
	choice := MoodChoice{"drizzle", "night", "far", "soothe"}
	task := Task{Prompt: "a cup of tea", Language: i18n.En, MoodChoice: &choice}
	s.prepareMoodTask(&task, m, concept, false)
	if hasHan(task.Mood) || hasHan(task.Concept) || hasHan(task.Prompt) || !strings.Contains(task.Prompt, "Atmosphere:") {
		t.Fatalf("%q %q %q", task.Mood, task.Concept, task.Prompt)
	}
	// Started in Chinese, it stays Chinese after a switch to English.
	zh := Task{Prompt: "一杯茶", Language: i18n.ZhCN, MoodChoice: &choice}
	s.prepareMoodTask(&zh, m, nil, false)
	if zh.Mood != "细雨 · 深夜 · 很远 · 缓一缓" || !strings.Contains(zh.Prompt, "画面气氛") {
		t.Fatalf("%q %q", zh.Mood, zh.Prompt)
	}
}
