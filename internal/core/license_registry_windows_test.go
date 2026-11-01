//go:build windows

package core

import "golang.org/x/sys/windows/registry"

func deleteTestRegistryKey(key string) { registry.DeleteKey(registry.CURRENT_USER, key) }
