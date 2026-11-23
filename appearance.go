package main

import (
	"context"

	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
	"github.com/wailsapp/wails/v2/pkg/runtime"

	"imageworkbench/internal/core"
)

// The window's own colours for 明 and 暗 (0.8.3). They match `background` and
// `text` in frontend/src/tokens.ts, so the native title bar and the page are
// one surface. Windows 11 draws these colours; Windows 10 only switches the
// title bar between its own black and white.
var (
	darkWall   = options.RGBA{R: 0x16, G: 0x13, B: 0x0F, A: 255}
	darkInk    = options.RGBA{R: 0xEF, G: 0xE8, B: 0xDA, A: 255}
	darkFaint  = options.RGBA{R: 0xA6, G: 0x9C, B: 0x8C, A: 255}
	lightWall  = options.RGBA{R: 0xF2, G: 0xED, B: 0xE3, A: 255}
	lightInk   = options.RGBA{R: 0x1F, G: 0x1B, B: 0x16, A: 255}
	lightFaint = options.RGBA{R: 0x6B, G: 0x62, B: 0x53, A: 255}
)

func rgb(c options.RGBA) int32 { return windows.RGB(c.R, c.G, c.B) }

// titleBarTheme colours the title bar and border like the page, in both modes.
func titleBarTheme() *windows.ThemeSettings {
	return &windows.ThemeSettings{
		DarkModeTitleBar:           rgb(darkWall),
		DarkModeTitleBarInactive:   rgb(darkWall),
		DarkModeTitleText:          rgb(darkInk),
		DarkModeTitleTextInactive:  rgb(darkFaint),
		DarkModeBorder:             rgb(darkWall),
		DarkModeBorderInactive:     rgb(darkWall),
		LightModeTitleBar:          rgb(lightWall),
		LightModeTitleBarInactive:  rgb(lightWall),
		LightModeTitleText:         rgb(lightInk),
		LightModeTitleTextInactive: rgb(lightFaint),
		LightModeBorder:            rgb(lightWall),
		LightModeBorderInactive:    rgb(lightWall),
	}
}

// windowTheme maps the saved appearance to the window theme Wails starts with;
// SystemDefault follows Windows, also when it changes while the app is open.
func windowTheme(appearance string) windows.Theme {
	switch appearance {
	case core.AppearanceLight:
		return windows.Light
	case core.AppearanceDark:
		return windows.Dark
	}
	return windows.SystemDefault
}

// windowBackground is the colour the window shows before the page paints, so
// a light start does not flash the dark wall.
func windowBackground(appearance string) *options.RGBA {
	light := appearance == core.AppearanceLight || (appearance == core.AppearanceSystem && !systemPrefersDark())
	if light {
		c := lightWall
		return &c
	}
	c := darkWall
	return &c
}

func applyWindowAppearance(ctx context.Context, appearance string) {
	switch windowTheme(appearance) {
	case windows.Light:
		runtime.WindowSetLightTheme(ctx)
	case windows.Dark:
		runtime.WindowSetDarkTheme(ctx)
	default:
		runtime.WindowSetSystemDefaultTheme(ctx)
	}
	bg := windowBackground(appearance)
	runtime.WindowSetBackgroundColour(ctx, bg.R, bg.G, bg.B, bg.A)
}

func modeName(dark bool) string {
	if dark {
		return core.AppearanceDark
	}
	return core.AppearanceLight
}

// SystemAppearance is the Windows app mode now: light or dark. The page asks
// for it at start and hears "appearance:system" when it changes.
func (a *App) SystemAppearance() string { return modeName(systemPrefersDark()) }

// watchAppearance tells the page when Windows switches light/dark, for
// appearance "system". Wails recolours the title bar by itself; the window's
// own background follows here.
func (a *App) watchAppearance(ctx context.Context) {
	go watchSystemTheme(ctx, func(dark bool) {
		if a.service.Appearance() == core.AppearanceSystem {
			bg := windowBackground(core.AppearanceSystem)
			runtime.WindowSetBackgroundColour(ctx, bg.R, bg.G, bg.B, bg.A)
		}
		runtime.EventsEmit(ctx, "appearance:system", modeName(dark))
	})
}
