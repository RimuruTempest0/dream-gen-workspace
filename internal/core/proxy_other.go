//go:build !windows

package core

func systemProxyConfig() (string, string) { return "", "" }
