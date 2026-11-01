// Package packcheck fails a release build when the shipping ZIP could carry the
// developer's own configuration: data or profile files, or any LLM / image relay
// connection detail (address, name, API key in plain or DPAPI-encrypted form).
// Customer packages may also carry customer.dat, and must not contain any of
// that customer's keys in plain text. Values are never printed; only the kind of
// finding and the ZIP entry are.
package packcheck

import (
	"archive/zip"
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf16"

	"imageworkbench/internal/core"
	_ "modernc.org/sqlite"
)

// shipping lists every path the clean package may contain; anything else fails.
var shipping = []string{"ImageWorkbench.exe", "product-browser.exe", "使用说明.txt", "version.json", "runtime/", "agent/claude-code/claude.exe", "agent/claude-code/version.json", "agent/claude-code/LICENSE.md"}

// vendor files are copied unchanged and verified by signature or hash in build.ps1.
// A connection name is often a common word ("DeepSeek"), which these files can
// legitimately contain, so they are searched for addresses and keys only.
var vendor = []string{"runtime/", "agent/claude-code/claude.exe"}

type marker struct {
	kind     string
	value    []byte
	ownFiles bool // only searched in files this project produces
}

const customerFile = "customer.dat"

// Secret is a value that must not appear in the package in plain text, such as
// a customer's API key. Kind names it in findings; the value is never printed.
type Secret struct {
	Kind  string
	Value string
}

type Options struct {
	// DataDir is the developer's own data directory; its connections must not ship.
	DataDir string
	// CustomerFile allows customer.dat at the package root (customer packages).
	CustomerFile bool
	// Secrets must not appear anywhere in the package in plain text.
	Secrets []Secret
}

// Check returns findings; an empty list means the package is clean.
func Check(archive string, o Options) ([]string, error) {
	var markers []marker
	if o.DataDir != "" {
		var e error
		if markers, e = connectionMarkers(o.DataDir); e != nil {
			return nil, e
		}
	}
	for _, s := range o.Secrets {
		if v := strings.TrimSpace(s.Value); len(v) >= 6 {
			markers = append(markers, marker{s.Kind, []byte(v), false}, marker{s.Kind, utf16le(v), false})
		}
	}
	return check(archive, markers, o)
}

func check(archive string, markers []marker, o Options) ([]string, error) {
	r, e := zip.OpenReader(archive)
	if e != nil {
		return nil, e
	}
	defer r.Close()
	var findings []string
	for _, f := range r.File {
		name := strings.ReplaceAll(f.Name, `\`, "/")
		if !allowed(name) && !(o.CustomerFile && name == customerFile) {
			findings = append(findings, "unexpected file in shipping ZIP: "+name)
			continue
		}
		if f.FileInfo().IsDir() || len(markers) == 0 {
			continue
		}
		search := markers
		if isVendor(name) {
			search = nil
			for _, m := range markers {
				if !m.ownFiles {
					search = append(search, m)
				}
			}
		}
		found, e := scan(f, search)
		if e != nil {
			return nil, fmt.Errorf("%s: %w", name, e)
		}
		for _, kind := range found {
			findings = append(findings, fmt.Sprintf("%s found in %s", kind, name))
		}
	}
	return findings, nil
}

func allowed(name string) bool {
	for _, p := range shipping {
		if name == p || strings.HasSuffix(p, "/") && strings.HasPrefix(name, p) {
			return true
		}
	}
	return name == "runtime" || name == "agent/" || name == "agent/claude-code/"
}

func isVendor(name string) bool {
	for _, p := range vendor {
		if name == p || strings.HasSuffix(p, "/") && strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

// connectionMarkers reads the connections read-only; the app may be running.
func connectionMarkers(root string) ([]marker, error) {
	db, e := sql.Open("sqlite", "file:"+filepath.ToSlash(filepath.Join(root, "app.db"))+"?mode=ro")
	if e != nil {
		return nil, e
	}
	defer db.Close()
	rows, e := db.Query(`SELECT body FROM objects WHERE kind='connection'`)
	if e != nil {
		return nil, fmt.Errorf("read connections: %w", e)
	}
	defer rows.Close()
	var markers []marker
	add := func(kind, v string, ownFiles bool) {
		if v = strings.TrimSpace(v); len(v) >= 6 {
			markers = append(markers, marker{kind, []byte(v), ownFiles}, marker{kind, utf16le(v), ownFiles})
		}
	}
	for rows.Next() {
		var body string
		var c core.Connection
		if e = rows.Scan(&body); e != nil {
			return nil, e
		}
		if e = json.Unmarshal([]byte(body), &c); e != nil {
			return nil, e
		}
		label := "image relay connection"
		if c.Kind == "llm" {
			label = "LLM connection"
		}
		add(label+" name", c.Name, true)
		base := strings.TrimRight(strings.TrimSpace(c.BaseURL), "/")
		add(label+" address", strings.TrimPrefix(strings.TrimPrefix(base, "https://"), "http://"), false)
		if u, e := url.Parse(base); e == nil {
			add(label+" host", u.Hostname(), false)
		}
		if c.Credential == "" {
			continue
		}
		sealed, e := os.ReadFile(filepath.Join(root, "secrets", c.Credential+".bin"))
		if errors.Is(e, os.ErrNotExist) {
			continue
		} else if e != nil {
			return nil, e
		}
		markers = append(markers, marker{label + " encrypted key", sealed, false})
		if plain, e := core.Unprotect(sealed); e == nil {
			add(label+" API key", string(plain), false)
		}
	}
	return markers, rows.Err()
}

func utf16le(s string) []byte {
	var b bytes.Buffer
	for _, u := range utf16.Encode([]rune(s)) {
		b.WriteByte(byte(u))
		b.WriteByte(byte(u >> 8))
	}
	return b.Bytes()
}

// scan streams one entry, keeping an overlap so a value split across reads is found.
func scan(f *zip.File, markers []marker) ([]string, error) {
	if len(markers) == 0 {
		return nil, nil
	}
	rc, e := f.Open()
	if e != nil {
		return nil, e
	}
	defer rc.Close()
	longest := 0
	for _, m := range markers {
		longest = max(longest, len(m.value))
	}
	seen := map[string]bool{}
	buf := make([]byte, 0, 4<<20+longest)
	chunk := make([]byte, 4<<20)
	for {
		n, e := rc.Read(chunk)
		buf = append(buf, chunk[:n]...)
		for _, m := range markers {
			if !seen[m.kind] && bytes.Contains(buf, m.value) {
				seen[m.kind] = true
			}
		}
		if len(buf) > longest {
			buf = append(buf[:0], buf[len(buf)-longest:]...)
		}
		if e == io.EOF {
			break
		} else if e != nil {
			return nil, e
		}
	}
	var kinds []string
	for k := range seen {
		kinds = append(kinds, k)
	}
	return kinds, nil
}
