//go:build windows

package core

import "golang.org/x/sys/windows/registry"

const licenseRegistryPath = `Software\ImageWorkbench\Licenses`

// licenseRegistryKey lets tests use their own registry key.
var licenseRegistryKey = licenseRegistryPath

func licenseRegistryRead(softwareID string) ([]byte, bool) {
	k, e := registry.OpenKey(registry.CURRENT_USER, licenseRegistryKey, registry.QUERY_VALUE)
	if e != nil {
		return nil, false
	}
	defer k.Close()
	v, _, e := k.GetBinaryValue(softwareID)
	return v, e == nil
}

func licenseRegistryWrite(softwareID string, sealed []byte) error {
	k, _, e := registry.CreateKey(registry.CURRENT_USER, licenseRegistryKey, registry.SET_VALUE)
	if e != nil {
		return e
	}
	defer k.Close()
	return k.SetBinaryValue(softwareID, sealed)
}
