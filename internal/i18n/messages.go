package i18n

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"strings"
	"sync"
)

// ---- backend messages (0.8.0) -------------------------------------------------
//
// Messages the backend returns to the interface (errors, results, task notes,
// log lines) live in messages/<area>.json, laid out like the frontend text
// tables: {"zh-CN": {"<area>.<name>": "…"}, "en": {…}, "vi": …, "ja": …, "ko": …}.
// Placeholders are {name}. The catalogs are checked by messages_test.go
// (same rules as the frontend tables) and by TestBackendMessageCalls, which
// reads the Go sources and checks every call's key and placeholder names.
//
// A message is put together in the language current when it is written and
// stored as finished text (ADR 0004). Upstream error text is passed in as a
// placeholder value and is never translated.

//go:embed messages/*.json
var messageFiles embed.FS

var (
	catalogOnce sync.Once
	catalog     map[string]map[string]string // lang → key → text
)

func messages() map[string]map[string]string {
	catalogOnce.Do(func() {
		catalog = map[string]map[string]string{}
		paths, _ := fs.Glob(messageFiles, "messages/*.json")
		for _, p := range paths {
			b, e := messageFiles.ReadFile(p)
			if e != nil {
				panic("i18n: " + p + ": " + e.Error())
			}
			var tables map[string]map[string]string
			if e = json.Unmarshal(b, &tables); e != nil {
				panic("i18n: " + path.Base(p) + ": " + e.Error())
			}
			for lang, table := range tables {
				if catalog[lang] == nil {
					catalog[lang] = map[string]string{}
				}
				for k, v := range table {
					catalog[lang][k] = v
				}
			}
		}
	})
	return catalog
}

var argPattern = regexp.MustCompile(`\{(\w+)\}`)

// T returns the backend message key in lang with its {name} placeholders
// filled from args, given as name, value pairs:
//
//	i18n.T(lang, "task.count", "max", 20)
//
// A key missing in lang falls back to zh-CN; an unknown key returns the key.
// Values are written with fmt.Sprint and are never translated.
func T(lang, key string, args ...any) string {
	all := messages()
	text, ok := all[lang][key]
	if !ok {
		if text, ok = all[Default][key]; !ok {
			return key
		}
	}
	if len(args) == 0 {
		return text
	}
	values := map[string]string{}
	for i := 0; i+1 < len(args); i += 2 {
		name, _ := args[i].(string)
		values[name] = fmt.Sprint(args[i+1])
	}
	return argPattern.ReplaceAllStringFunc(text, func(m string) string {
		if v, ok := values[m[1:len(m)-1]]; ok {
			return v
		}
		return m
	})
}

// Variants returns the text of key in every interface language, for code that
// must recognise stored text written in any of them (it keeps its language).
func Variants(key string) []string {
	out := []string{}
	for _, lang := range Languages {
		if text, ok := messages()[lang][key]; ok {
			out = append(out, text)
		}
	}
	return out
}

// HasPrefix reports whether text starts with the message key (without
// placeholders) in any interface language.
func HasPrefix(text, key string) bool {
	for _, v := range Variants(key) {
		if strings.HasPrefix(text, v) {
			return true
		}
	}
	return false
}

type langKey struct{}

// WithLanguage carries the interface language to code that has a context but
// no Service (the provider, readers of HTTP replies).
func WithLanguage(ctx context.Context, lang string) context.Context {
	return context.WithValue(ctx, langKey{}, lang)
}

// FromContext is the language set by WithLanguage, or zh-CN.
func FromContext(ctx context.Context) string {
	if ctx != nil {
		if v, ok := ctx.Value(langKey{}).(string); ok && Supported(v) {
			return v
		}
	}
	return Default
}
