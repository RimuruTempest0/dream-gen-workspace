//go:build !windows

package i18n

import "os"

// SystemLanguages returns the user's language from the environment.
func SystemLanguages() []string {
	for _, k := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		if v := os.Getenv(k); v != "" {
			return []string{v}
		}
	}
	return nil
}
