//go:build windows

package admin

import (
	"testing"

	"golang.org/x/sys/windows/registry"

	"imageworkbench/internal/core"
)

// isolate keeps the app core's trial records of this test in a temp folder and a
// test registry key, removed afterwards.
func isolate(t *testing.T) {
	t.Helper()
	key := `Software\ImageWorkbench\Test\` + newID()
	restore := core.IsolateLicenseStateForTest(t.TempDir(), key)
	t.Cleanup(func() {
		restore()
		registry.DeleteKey(registry.CURRENT_USER, key)
	})
}
