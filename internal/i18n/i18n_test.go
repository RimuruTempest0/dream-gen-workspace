package i18n

import "testing"

func TestMatchSystemLanguage(t *testing.T) {
	for tag, want := range map[string]string{
		"ja-JP": Ja, "ko-KR": Ko, "vi-VN": Vi, "en-US": En, "en-GB": En,
		"zh-CN": ZhCN, "zh-Hans-CN": ZhCN, "zh-TW": ZhCN, "en_US.UTF-8": En,
		"de-DE": "", "fr": "", "": "",
	} {
		if got := Match(tag); got != want {
			t.Errorf("Match(%q) = %q, want %q", tag, got, want)
		}
	}
	if FromSystem("ja-JP", "en-US") != Ja || FromSystem("de-DE", "en-US") != ZhCN || FromSystem() != ZhCN {
		t.Error("FromSystem must use the first system language and fall back to Chinese")
	}
}

func TestGlossaryCoversEveryLanguage(t *testing.T) {
	seen := map[string]bool{}
	for _, term := range Glossary() {
		for _, l := range Languages {
			if term[l] == "" {
				t.Errorf("term %s has no %s translation", term[ZhCN], l)
			}
		}
		if seen[term[ZhCN]] {
			t.Errorf("term %s listed twice", term[ZhCN])
		}
		seen[term[ZhCN]] = true
	}
	if Translate("生图工作台", En) != "Image Workbench" || AppName(Ko) != "Image Workbench" || AppName(ZhCN) != "生图工作台" {
		t.Error("app name")
	}
}
