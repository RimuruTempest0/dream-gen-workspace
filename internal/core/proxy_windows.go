//go:build windows

package core

import "golang.org/x/sys/windows/registry"

func systemProxyConfig() (string, string) {
	k, e := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Internet Settings`, registry.QUERY_VALUE)
	if e != nil {
		return "", ""
	}
	defer k.Close()
	enabled, _, e := k.GetIntegerValue("ProxyEnable")
	if e != nil || enabled == 0 {
		return "", ""
	}
	server, _, e := k.GetStringValue("ProxyServer")
	if e != nil {
		return "", ""
	}
	bypass, _, _ := k.GetStringValue("ProxyOverride")
	return server, bypass
}
