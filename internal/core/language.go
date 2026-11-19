package core

import "imageworkbench/internal/i18n"

// ---- 界面语言 (0.8.0) --------------------------------------------------------
//
// The interface language is a local preference like image-concurrency. On the
// first start of a new data directory it follows the Windows UI language
// (mapped onto the five, otherwise Chinese) and is saved; after that the saved
// preference wins. A data directory from before 0.8.0 keeps Chinese, so an
// upgrade looks the same as before. The frontend switches it with
// SetLanguage; backend code reads it with Language at the moment it writes a
// message (stored text keeps the language it was written in, ADR 0004).

const interfaceLanguagePrefKey = "interface-language"

// systemUILanguages is the Windows UI language lookup; tests and the desktop
// QA build replace it.
var systemUILanguages = i18n.SystemLanguages

// Language is the current interface language code (zh-CN, en, vi, ja, ko).
// It never takes s.mu, so it is safe to call anywhere, also with s.mu held.
func (s *Service) Language() string {
	if v, ok := s.lang.Load().(string); ok && v != "" {
		return v
	}
	return i18n.Default
}

// SetLanguage saves the interface language. It applies to what is written
// from now on; work already started finishes in the language it began in.
func (s *Service) SetLanguage(code string) error {
	if !i18n.Supported(code) {
		return s.fail("app.languageInvalid")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if e := s.store.Put("preferences", interfaceLanguagePrefKey, code); e != nil {
		return e
	}
	s.lang.Store(code)
	return nil
}

// loadLanguage reads the saved interface language, or decides and saves it on
// the first start. Call it before anything else writes preferences.
func loadLanguage(st *Store) (string, error) {
	var saved string
	if st.Get("preferences", interfaceLanguagePrefKey, &saved) == nil && i18n.Supported(saved) {
		return saved, nil
	}
	lang := i18n.FromSystem(systemUILanguages()...)
	// Anything already saved (preferences, connections, tasks, assets, logs, …)
	// means this data directory was used before 0.8.0.
	used, e := st.hasData()
	if e != nil {
		return "", e
	}
	if used {
		lang = i18n.Default
	}
	return lang, st.Put("preferences", interfaceLanguagePrefKey, lang)
}

// hasData reports whether the database holds any record at all: a saved
// object of any kind, a log line, a submission or an Agent request.
func (st *Store) hasData() (bool, error) {
	var used bool
	e := st.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM objects) OR EXISTS(SELECT 1 FROM app_logs) OR EXISTS(SELECT 1 FROM submissions) OR EXISTS(SELECT 1 FROM agent_requests)`).Scan(&used)
	return used, e
}
