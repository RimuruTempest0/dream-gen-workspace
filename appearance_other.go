//go:build !windows

package main

import "context"

func systemPrefersDark() bool { return true }

func watchSystemTheme(ctx context.Context, changed func(dark bool)) {}
