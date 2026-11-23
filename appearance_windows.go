//go:build windows

package main

import (
	"context"
	"runtime"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const personalizeKey = `SOFTWARE\Microsoft\Windows\CurrentVersion\Themes\Personalize`

// systemPrefersDark reports the Windows "app mode" (Settings > Personalisation
// > Colours), the same setting Wails reads for the title bar. The page does not
// use prefers-color-scheme for this: the embedded WebView2 does not reliably
// report it (seen 0.8.3: light while Windows was dark).
func systemPrefersDark() bool {
	k, e := registry.OpenKey(registry.CURRENT_USER, personalizeKey, registry.QUERY_VALUE)
	if e != nil {
		return false
	}
	defer k.Close()
	light, _, e := k.GetIntegerValue("AppsUseLightTheme")
	return e == nil && light == 0
}

// watchSystemTheme calls changed whenever the Windows app mode flips, until ctx
// ends. It waits on registry change notifications, so it costs nothing while
// nothing changes.
func watchSystemTheme(ctx context.Context, changed func(dark bool)) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	k, e := registry.OpenKey(registry.CURRENT_USER, personalizeKey, registry.NOTIFY|registry.QUERY_VALUE)
	if e != nil {
		return
	}
	defer k.Close()
	dark := systemPrefersDark()
	for ctx.Err() == nil {
		if windows.RegNotifyChangeKeyValue(windows.Handle(k), false, windows.REG_NOTIFY_CHANGE_LAST_SET, 0, false) != nil {
			return
		}
		if now := systemPrefersDark(); now != dark {
			dark = now
			changed(dark)
		}
	}
}
