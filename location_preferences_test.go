package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLocationPreferenceSurvivesRestartAndOverwrite(t *testing.T) {
	root := t.TempDir()
	for _, target := range []string{filepath.Join(root, "自选数据"), filepath.Join(t.TempDir(), "外部目录")} {
		if e := saveDataPath(root, target); e != nil {
			t.Fatal(e)
		}
		if got := savedDataPath(root); got != target {
			t.Fatalf("got %q want %q", got, target)
		}
	}
	os.WriteFile(filepath.Join(root, "data-location.txt"), []byte("  data\n"), 0600)
	if savedDataPath(root) != filepath.Join(root, "data") {
		t.Fatal("relative path not resolved")
	}
	if e := saveDataPath(filepath.Join(root, "missing"), "D:\\data"); e == nil {
		t.Fatal("write error ignored")
	}
}
