//go:build !windows

package core

var licenseRegistryKey = ""

func licenseRegistryRead(string) ([]byte, bool) { return nil, false }
func licenseRegistryWrite(string, []byte) error { return nil }
