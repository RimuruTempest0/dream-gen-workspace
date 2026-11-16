package admin

import (
	"archive/zip"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"os"
	"strings"

	"imageworkbench/internal/update"
)

// In-app updates (admin 1.2.0): the address customer apps read update.json from
// is built into every package made after it is set. Publishing an update means
// signing update.json for an update package and uploading both to that address.

// SetUpdateURL sets where customer apps read update.json ("" = no updates).
func (a *Admin) SetUpdateURL(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw != "" {
		u, e := url.Parse(raw)
		if e != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.RawQuery != "" || u.User != nil {
			return errors.New("更新清单地址应为 https:// 开头的完整地址，例如 https://example.com/imageworkbench/update.json")
		}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.d.UpdateURL = raw
	return a.saveLocked()
}

// UpdateRelease describes the update.json to sign.
type UpdateRelease struct {
	Zip        string `json:"zip"`        // update package on this computer
	URL        string `json:"url"`        // where customers download it
	MinVersion string `json:"minVersion"` // oldest version that may update in place
	Notes      string `json:"notes"`
}

// PublishUpdate signs update.json for an update package and writes it to dest.
func (a *Admin) PublishUpdate(r UpdateRelease, dest string) (update.Manifest, error) {
	version, e := packageVersion(r.Zip)
	if e != nil {
		return update.Manifest{}, e
	}
	sum, e := update.FileSHA256(r.Zip)
	if e != nil {
		return update.Manifest{}, e
	}
	info, e := os.Stat(r.Zip)
	if e != nil {
		return update.Manifest{}, e
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	priv, e := a.signingKeyLocked()
	if e != nil {
		return update.Manifest{}, e
	}
	m := update.Manifest{Version: version, MinVersion: strings.TrimSpace(r.MinVersion), Notes: strings.TrimSpace(r.Notes), URL: strings.TrimSpace(r.URL), SHA256: sum, Size: info.Size()}
	data, e := update.Sign(m, priv)
	if e != nil {
		return update.Manifest{}, e
	}
	if e = os.WriteFile(dest, data, 0o644); e != nil {
		return update.Manifest{}, e
	}
	return update.Verify(data, []string{a.d.SigningPublicKey}, "")
}

// packageVersion reads version.json from an update package and checks that the
// package holds only program files.
func packageVersion(path string) (string, error) {
	z, e := zip.OpenReader(path)
	if e != nil {
		return "", errors.New("请选择程序更新包（ImageWorkbench-update-….zip）")
	}
	defer z.Close()
	version := ""
	for _, f := range z.File {
		name := strings.ReplaceAll(f.Name, string(os.PathSeparator), "/")
		if f.FileInfo().IsDir() {
			continue
		}
		if !update.AllowedEntry(name) {
			return "", errors.New("这个 ZIP 不是程序更新包：包含 " + name)
		}
		if name == "version.json" {
			r, e := f.Open()
			if e != nil {
				return "", e
			}
			var v struct {
				Version string `json:"version"`
			}
			b, e := io.ReadAll(io.LimitReader(r, 1<<20))
			r.Close()
			if e != nil || json.Unmarshal(update.TrimBOM(b), &v) != nil {
				return "", errors.New("更新包的 version.json 无法读取")
			}
			version = v.Version
		}
	}
	if version == "" {
		return "", errors.New("更新包里没有 version.json")
	}
	return version, nil
}

func fileExists(path string) bool {
	_, e := os.Stat(path)
	return e == nil
}
