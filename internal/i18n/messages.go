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
// Values are inserted as they are and never translated, except catalog errors
// (*Error) and Messages, which are written in lang too (see Text).
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
		values[name] = Text(args[i+1], lang)
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

// Message is a catalog message that is not yet put into a language: packages
// that do not know the interface language (update, license) return it inside
// an *Error, and code that must show the same state in whatever language is
// current at display time keeps it (the licence status).
type Message struct {
	Key  string
	Args []any // name, value pairs as for T
}

// Msg builds a Message; see T for the arguments.
func Msg(key string, args ...any) Message { return Message{Key: key, Args: args} }

// IsZero reports whether no message is set.
func (m Message) IsZero() bool { return m.Key == "" }

// In is the message in lang. Values that are catalog errors (*Error) or
// Messages are put into lang too; other values are inserted as is.
func (m Message) In(lang string) string {
	if m.Key == "" {
		return ""
	}
	return T(lang, m.Key, m.Args...)
}

// Error is a catalog message used as an error. Error() is the zh-CN text (the
// admin tool and logs written without a language); show it with Text.
type Error struct{ Message }

// Fail returns a catalog message as an error:
//
//	return i18n.Fail("update.tooLarge")
func Fail(key string, args ...any) error { return &Error{Msg(key, args...)} }

func (e *Error) Error() string { return e.In(Default) }

// Unwrap returns the first error among the values, so errors.Is still sees
// a wrapped cause.
func (e *Error) Unwrap() error {
	for i := 1; i < len(e.Args); i += 2 {
		if err, ok := e.Args[i].(error); ok {
			return err
		}
	}
	return nil
}

// Localized is a value that can write itself in any interface language
// (Message, *Error, and error types of other packages built on them).
type Localized interface{ In(lang string) string }

// Text writes a placeholder value in lang: a Localized value (the value itself,
// not one wrapped inside other text) or a Message in lang, an other error as
// its own text, anything else with fmt.Sprint.
func Text(v any, lang string) string {
	switch v := v.(type) {
	case nil:
		return ""
	case Localized:
		return v.In(lang)
	case error:
		return v.Error()
	}
	return fmt.Sprint(v)
}
