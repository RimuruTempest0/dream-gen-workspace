//go:build windows

package i18n

import "golang.org/x/sys/windows"

// SystemLanguages returns the Windows user's preferred UI languages, most
// preferred first (for example "ja-JP").
func SystemLanguages() []string {
	tags, e := windows.GetUserPreferredUILanguages(windows.MUI_LANGUAGE_NAME)
	if e != nil {
		return nil
	}
	return tags
}
