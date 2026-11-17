package core

import (
	"encoding/json"
	"os"
	"regexp"
	"strings"
	"testing"
)

// AppVersion is what updates compare against; 0.7.33 and 0.7.34 shipped still
// reporting 0.7.32. Every place that carries the version must agree with it.
func TestAppVersionMatchesEveryVersionString(t *testing.T) {
	read := func(path string) string {
		b, e := os.ReadFile("../../" + path)
		if e != nil {
			t.Fatal(e)
		}
		return string(b)
	}
	var wails struct {
		Info struct {
			ProductVersion string `json:"productVersion"`
		} `json:"info"`
	}
	if e := json.Unmarshal([]byte(read("wails.json")), &wails); e != nil {
		t.Fatal(e)
	}
	var pkg struct {
		Version string `json:"version"`
	}
	if e := json.Unmarshal([]byte(read("frontend/package.json")), &pkg); e != nil {
		t.Fatal(e)
	}
	build := regexp.MustCompile(`\[string\]\$Version = '([^']+)'`).FindStringSubmatch(strings.TrimPrefix(read("scripts/build.ps1"), "\xef\xbb\xbf"))
	app := regexp.MustCompile(`summary\.data\?\.version \|\| "([^"]+)"`).FindStringSubmatch(read("frontend/src/App.tsx"))
	if build == nil || app == nil {
		t.Fatal("version default not found in build.ps1 or App.tsx")
	}
	for name, v := range map[string]string{"wails.json": wails.Info.ProductVersion, "frontend/package.json": pkg.Version, "scripts/build.ps1": build[1], "frontend/src/App.tsx": app[1]} {
		if v != AppVersion {
			t.Errorf("%s says %s, AppVersion is %s", name, v, AppVersion)
		}
	}
}
