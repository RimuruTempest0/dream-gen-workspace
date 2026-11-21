//go:build !windows

package main

import (
	"os"
	"path/filepath"
)

// desktopDir is the user's desktop folder; "" lets the dialog choose.
func desktopDir() string {
	home, e := os.UserHomeDir()
	if e != nil {
		return ""
	}
	return filepath.Join(home, "Desktop")
}
