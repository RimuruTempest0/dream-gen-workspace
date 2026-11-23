package core

// ---- 外观：明 / 暗 (0.8.3) -------------------------------------------------
//
// The appearance is a local preference like the interface language: "system"
// follows the Windows light/dark app setting, "light" (明) and "dark" (暗) are
// fixed by the customer clicking the mark in the top-left corner or choosing
// on the settings page. A new data directory starts on "system"; one used
// before 0.8.3 keeps "dark", so an upgrade looks the same as before.

const appearancePrefKey = "appearance"

const (
	AppearanceSystem = "system"
	AppearanceLight  = "light"
	AppearanceDark   = "dark"
)

func validAppearance(v string) bool {
	return v == AppearanceSystem || v == AppearanceLight || v == AppearanceDark
}

// Appearance is the saved appearance: system, light or dark. It never takes
// s.mu.
func (s *Service) Appearance() string {
	if v, ok := s.appearance.Load().(string); ok && validAppearance(v) {
		return v
	}
	return AppearanceSystem
}

// SetAppearance saves the appearance.
func (s *Service) SetAppearance(v string) error {
	if !validAppearance(v) {
		return s.fail("app.appearanceInvalid")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if e := s.store.Put("preferences", appearancePrefKey, v); e != nil {
		return e
	}
	s.appearance.Store(v)
	return nil
}

// loadAppearance reads the saved appearance, or decides and saves it on the
// first start. used says whether the data directory held anything before this
// start; the caller checks it before any first-start preference is saved.
func loadAppearance(st *Store, used bool) (string, error) {
	var saved string
	if st.Get("preferences", appearancePrefKey, &saved) == nil && validAppearance(saved) {
		return saved, nil
	}
	v := AppearanceSystem
	if used {
		v = AppearanceDark
	}
	return v, st.Put("preferences", appearancePrefKey, v)
}
