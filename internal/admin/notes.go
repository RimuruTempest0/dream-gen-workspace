package admin

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"imageworkbench/internal/core"
	"imageworkbench/internal/i18n"
)

// notesPrompt asks for the English update notes (admin 1.10.0). Fixed terms
// take their glossary translation, so the notes use the app's own words.
func notesPrompt(zh string) string {
	var terms []string
	for _, t := range i18n.Glossary() {
		if t[i18n.ZhCN] != "" && t[i18n.En] != "" {
			terms = append(terms, t[i18n.ZhCN]+" = "+t[i18n.En])
		}
	}
	return "Translate these update notes of a Windows app from Chinese into English. " +
		"The people using the app read them before a one-click update. The app is called Image Workbench in English (生图工作台 in Chinese). " +
		"Keep the meaning, the line breaks and any list markers; write plain, friendly English; do not add anything. " +
		"Use these fixed translations of the app's terms:\n" + strings.Join(terms, "\n") +
		"\n\nReply with the English notes only.\n\nNotes:\n" + zh
}

// translationKeyLocked picks the key the translation runs on: a verified LLM
// key of the pool that is not with a customer (its quota is theirs) and not
// revoked or disabled.
func (a *Admin) translationKeyLocked() (PoolKey, Upstream, error) {
	for _, k := range a.d.Keys {
		if k.Kind != "llm" || !k.Verified || k.Status != "available" {
			continue
		}
		if u, e := a.upstreamLocked(k.UpstreamID); e == nil {
			return k, *u, nil
		}
	}
	return PoolKey{}, Upstream{}, errors.New("Key 池里没有可用且通过能力验证的 LLM Key，无法翻译。可以导入并验证一个 LLM Key 后再试，或直接填写英文更新说明")
}

// TranslateNotes translates the Chinese update notes into English with an LLM
// key of the pool (one short, billable call; the UI asks first). The provider
// reads and may edit the result before the build signs it.
func (a *Admin) TranslateNotes(zh string) (string, error) {
	zh = strings.TrimSpace(zh)
	if zh == "" {
		return "", errors.New("请先填写中文更新说明")
	}
	a.mu.Lock()
	k, u, e := a.translationKeyLocked()
	var key []byte
	if e == nil {
		key, e = a.secret(k.SecretRef)
	}
	a.mu.Unlock()
	if e != nil {
		return "", e
	}
	tmp, e := os.MkdirTemp("", "iw-admin-notes-")
	if e != nil {
		return "", e
	}
	defer os.RemoveAll(tmp)
	s, e := core.NewService(filepath.Join(tmp, "data"))
	if e != nil {
		return "", e
	}
	defer s.Dispose()
	c, e := s.SaveConnection(core.ConnectionInput{Kind: "llm", Name: u.Name, BaseURL: u.BaseURL, Model: u.Model, Key: string(key), Mode: u.Mode, AuthMode: u.AuthMode, MaxOutputTokens: u.MaxOutputTokens})
	if e != nil {
		return "", e
	}
	en, e := s.AskText(c.ID, "翻译更新说明", notesPrompt(zh))
	if e != nil {
		return "", errors.New("翻译失败：" + e.Error())
	}
	return strings.TrimSpace(en), nil
}
