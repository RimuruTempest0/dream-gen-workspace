package core

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// The trial counter and the activation are also kept outside the data folder,
// per software ID, so reinstalling the program or deleting its data does not
// reset the trial: a DPAPI-sealed file in the user's profile and a registry value
// (licenseRegistry, Windows only). The highest count wins. This makes casual
// resets fail; the hard limit is still each key's quota at the upstream.

type sharedLicense struct {
	Used int    `json:"used"`
	Code string `json:"code,omitempty"` // the activation code once activated
}

// licenseStateDir holds the per-software-ID files; tests point it elsewhere.
var licenseStateDir = func() string {
	base, e := os.UserConfigDir()
	if e != nil {
		base = os.TempDir()
	}
	return filepath.Join(base, "ImageWorkbench", "licenses")
}

var errLicenseTampered = errors.New("试用记录被改动")

// IsolateLicenseStateForTest keeps trial records of this process in dir and the
// given registry key (under HKCU), so tests never touch the user's own records.
func IsolateLicenseStateForTest(dir, registryKey string) (restore func()) {
	oldDir, oldKey := licenseStateDir, licenseRegistryKey
	licenseStateDir = func() string { return dir }
	licenseRegistryKey = registryKey
	return func() { licenseStateDir, licenseRegistryKey = oldDir, oldKey }
}

func sealShared(v sharedLicense) ([]byte, error) {
	b, _ := json.Marshal(v)
	return Protect(b)
}

func openShared(sealed []byte) (sharedLicense, error) {
	var v sharedLicense
	plain, e := Unprotect(sealed)
	if e != nil || json.Unmarshal(plain, &v) != nil {
		return v, errLicenseTampered
	}
	return v, nil
}

// loadShared merges every copy; a copy that cannot be read counts as tampered.
func loadShared(softwareID string) (sharedLicense, bool) {
	out, tampered := sharedLicense{}, false
	merge := func(sealed []byte, ok bool) {
		if !ok {
			return
		}
		v, e := openShared(sealed)
		if e != nil {
			tampered = true
			return
		}
		out.Used = max(out.Used, v.Used)
		if v.Code != "" {
			out.Code = v.Code
		}
	}
	b, e := os.ReadFile(filepath.Join(licenseStateDir(), softwareID+".bin"))
	merge(b, e == nil)
	merge(licenseRegistryRead(softwareID))
	return out, tampered
}

func saveShared(softwareID string, v sharedLicense) error {
	sealed, e := sealShared(v)
	if e != nil {
		return e
	}
	dir := licenseStateDir()
	if e = os.MkdirAll(dir, 0o700); e != nil {
		return e
	}
	e = atomicWrite(filepath.Join(dir, softwareID+".bin"), sealed)
	if r := licenseRegistryWrite(softwareID, sealed); e == nil {
		e = r
	}
	return e
}
