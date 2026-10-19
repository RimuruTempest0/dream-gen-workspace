//go:build desktopqa

package core

import "time"

// The desktop QA build reads mock product pages from the local test server and
// shows the verification window sooner, so the end-to-end test stays short.
func init() {
	qaLocalProduct = true
	productBrowserChallengeAfter = 3 * time.Second
}
