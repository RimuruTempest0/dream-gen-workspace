//go:build windows

package main

import "golang.org/x/sys/windows"

// desktopDir is the user's desktop folder, wherever Windows keeps it (it may be
// moved, e.g. into OneDrive); "" lets the dialog choose.
func desktopDir() string {
	path, e := windows.KnownFolderPath(windows.FOLDERID_Desktop, 0)
	if e != nil {
		return ""
	}
	return path
}
