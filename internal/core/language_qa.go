//go:build desktopqa

package core

import "os"

// The desktop QA instance acts as if Windows were Chinese, whatever the test
// machine runs, unless IMAGEWORKBENCH_QA_SYSTEM_LANGUAGE or a test says otherwise.
func init() { SetTestSystemLanguage(os.Getenv("IMAGEWORKBENCH_QA_SYSTEM_LANGUAGE")) }

// SetTestSystemLanguage sets the Windows UI language the QA instance sees on
// the first start of a new data directory ("" = zh-CN).
func SetTestSystemLanguage(tag string) {
	if tag == "" {
		tag = "zh-CN"
	}
	systemUILanguages = func() []string { return []string{tag} }
}
