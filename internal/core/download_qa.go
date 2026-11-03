//go:build desktopqa

package core

import "time"

// The desktop QA build re-downloads failed results sooner, so the end-to-end
// test sees both the automatic re-download and its final failure quickly.
func init() {
	autoDownloadDelays = []time.Duration{2 * time.Second, 2 * time.Second}
}
