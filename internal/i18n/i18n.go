// Package i18n holds the 界面语言 (interface language) codes shared by the
// program and its tests, maps a Windows UI language onto them, and keeps the
// fixed-term glossary (GLOSSARY.md is the source of the terms).
//
// The interface text itself lives in the frontend text tables
// (frontend/src/i18n/messages/*.json); messages_test.go checks them.
package i18n

import "strings"

// The five interface languages. zh-CN is the default and the fallback.
const (
	ZhCN    = "zh-CN"
	En      = "en"
	Vi      = "vi"
	Ja      = "ja"
	Ko      = "ko"
	Default = ZhCN
)

// Languages lists the interface languages in the order the switch shows them.
var Languages = []string{ZhCN, En, Vi, Ja, Ko}

// Supported reports whether code is exactly one of the five language codes.
func Supported(code string) bool {
	for _, l := range Languages {
		if l == code {
			return true
		}
	}
	return false
}

// Match maps a system language tag (BCP 47 or a Windows name such as
// "ja-JP", "zh-Hans-CN", "en_US.UTF-8") onto an interface language.
// It returns "" when the language is none of the five.
func Match(tag string) string {
	tag = strings.ToLower(strings.TrimSpace(tag))
	if i := strings.IndexAny(tag, ".@"); i >= 0 {
		tag = tag[:i]
	}
	primary, _, _ := strings.Cut(strings.ReplaceAll(tag, "_", "-"), "-")
	switch primary {
	case "zh":
		return ZhCN
	case "en":
		return En
	case "vi":
		return Vi
	case "ja":
		return Ja
	case "ko":
		return Ko
	}
	return ""
}

// FromSystem picks the interface language for a first start from the system
// UI languages, most preferred first. Only the first one counts: a German
// Windows that also lists English as a fallback gets the default (Chinese).
func FromSystem(tags ...string) string {
	if len(tags) > 0 {
		if l := Match(tags[0]); l != "" {
			return l
		}
	}
	return Default
}

// AppName is the program name shown in the given interface language: 生图工作台
// in Chinese, the brand name Image Workbench in every other language.
func AppName(lang string) string {
	if lang == ZhCN {
		return "生图工作台"
	}
	return "Image Workbench"
}

// IsChinese reports whether the interface language is Chinese.
func IsChinese(lang string) bool { return lang == ZhCN }
