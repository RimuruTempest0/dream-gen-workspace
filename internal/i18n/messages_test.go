package i18n

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"
	"unicode"
)

// The frontend text tables: one JSON file per page or area, each holding the
// five language tables {"zh-CN": {...}, "en": {...}, "vi": ..., "ja": ..., "ko": ...}.
const tablesDir = "../../frontend/src/i18n/messages"

var placeholder = regexp.MustCompile(`\{(\w+)\}`)

// TestTextTablesComplete is the translation-completeness check: the five
// tables carry the same keys, the same placeholders, no empty or leftover
// Chinese text, and every fixed term uses its glossary translation.
func TestTextTablesComplete(t *testing.T) {
	paths, e := filepath.Glob(filepath.Join(tablesDir, "*.json"))
	if e != nil || len(paths) == 0 {
		t.Fatal("no text tables found in", tablesDir, e)
	}
	files := map[string][]byte{}
	for _, p := range paths {
		b, e := os.ReadFile(p)
		if e != nil {
			t.Fatal(e)
		}
		files[filepath.Base(p)] = b
	}
	for _, p := range checkTables(files, Glossary()) {
		t.Error(p)
	}
}

// The checker itself must catch each kind of mistake.
func TestTextTableCheckerCatchesMistakes(t *testing.T) {
	glossary := []Term{{ZhCN: "心境", En: "mood", Vi: "tâm trạng", Ja: "心境", Ko: "심경"}}
	table := func(zh, en, vi, ja, ko string) string {
		return fmt.Sprintf(`{"zh-CN":{%s},"en":{%s},"vi":{%s},"ja":{%s},"ko":{%s}}`, zh, en, vi, ja, ko)
	}
	ok := `"a.x":"你好 {name}"`
	vi, ja, ko := `"a.x":"Chào {name}"`, `"a.x":"やあ {name}"`, `"a.x":"안녕 {name}"`
	cases := map[string]struct{ file, want string }{
		"missing key":      {table(ok+`,"a.y":"再见"`, `"a.x":"Hi {name}"`, vi+`,"a.y":"Tạm biệt"`, ja+`,"a.y":"さよなら"`, ko+`,"a.y":"잘 가"`), `en: missing "a.y"`},
		"extra key":        {table(ok, `"a.x":"Hi {name}","a.z":"Z"`, vi, ja, ko), `en: "a.z" is not in zh-CN`},
		"placeholder":      {table(ok, `"a.x":"Hi {nam}"`, vi, ja, ko), `en "a.x": placeholders [nam], zh-CN has [name]`},
		"empty":            {table(ok, `"a.x":" "`, vi, ja, ko), `en "a.x": empty`},
		"leftover chinese": {table(ok, `"a.x":"Hi {name} 你好"`, vi, ja, ko), `en "a.x": contains Chinese`},
		"glossary":         {table(`"a.x":"心境 {name}"`, `"a.x":"Feeling {name}"`, `"a.x":"Tâm trạng {name}"`, `"a.x":"心境 {name}"`, `"a.x":"심경 {name}"`), `en "a.x": term 心境 must be "mood"`},
		"prefix":           {table(`"b.x":"你好"`, `"b.x":"Hi"`, `"b.x":"Chào"`, `"b.x":"やあ"`, `"b.x":"안녕"`), `"b.x" must start with "a."`},
		"duplicate":        {table(ok+`,"a.x":"又 {name}"`, `"a.x":"Hi {name}"`, vi, ja, ko), `zh-CN: "a.x" appears twice`},
		"language":         {`{"zh-CN":{` + ok + `},"en":{"a.x":"Hi {name}"}}`, `missing language vi`},
	}
	for name, c := range cases {
		problems := checkTables(map[string][]byte{"a.json": []byte(c.file)}, glossary)
		if !slices.ContainsFunc(problems, func(p string) bool { return strings.Contains(p, c.want) }) {
			t.Errorf("%s: want %q, got %q", name, c.want, problems)
		}
	}
	good := table(`"a.x":"心境 {name}"`, `"a.x":"Mood {name}"`, `"a.x":"Tâm trạng {name}"`, `"a.x":"心境 {name}"`, `"a.x":"심경 {name}"`)
	if p := checkTables(map[string][]byte{"a.json": []byte(good)}, glossary); len(p) > 0 {
		t.Error("good table rejected:", p)
	}
	two := map[string][]byte{"a.json": []byte(good), "b.json": []byte(strings.ReplaceAll(good, `"a.x"`, `"b.x"`))}
	if p := checkTables(two, glossary); len(p) > 0 {
		t.Error("two areas rejected:", p)
	}
}

func checkTables(files map[string][]byte, glossary []Term) []string {
	var problems []string
	add := func(format string, a ...any) { problems = append(problems, fmt.Sprintf(format, a...)) }
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	owner := map[string]string{}
	for _, name := range names {
		area := strings.TrimSuffix(name, ".json")
		tables, dups, e := decodeTables(files[name])
		if e != nil {
			add("%s: %v", name, e)
			continue
		}
		for _, d := range dups {
			add("%s %s", name, d)
		}
		for lang := range tables {
			if !Supported(lang) {
				add("%s: unknown language %s", name, lang)
			}
		}
		for _, lang := range Languages {
			if _, ok := tables[lang]; !ok {
				add("%s: missing language %s", name, lang)
			}
		}
		zh := tables[ZhCN]
		for _, key := range sortedKeys(zh) {
			if !strings.HasPrefix(key, area+".") {
				add("%s: key %q must start with %q", name, key, area+".")
			}
			if other, seen := owner[key]; seen {
				add("%s: key %q is also in %s", name, key, other)
			}
			owner[key] = name
		}
		for _, lang := range Languages {
			table, ok := tables[lang]
			if !ok {
				continue
			}
			for _, key := range sortedKeys(zh) {
				if _, ok := table[key]; !ok {
					add("%s %s: missing %q", name, lang, key)
				}
			}
			for _, key := range sortedKeys(table) {
				text := table[key]
				source, ok := zh[key]
				if !ok {
					add("%s %s: %q is not in zh-CN", name, lang, key)
					continue
				}
				if strings.TrimSpace(text) == "" {
					add("%s %s %q: empty", name, lang, key)
					continue
				}
				if got, want := placeholders(text), placeholders(source); !slices.Equal(got, want) {
					add("%s %s %q: placeholders %v, zh-CN has %v", name, lang, key, got, want)
				}
				if lang == En || lang == Vi || lang == Ko {
					if strings.ContainsFunc(text, func(r rune) bool { return unicode.Is(unicode.Han, r) }) {
						add("%s %s %q: contains Chinese: %s", name, lang, key, text)
					}
				}
				if lang == ZhCN {
					continue
				}
				for _, term := range glossary {
					want := term[lang]
					if strings.Contains(source, term[ZhCN]) && !strings.Contains(strings.ToLower(text), strings.ToLower(want)) {
						add("%s %s %q: term %s must be %q: %s", name, lang, key, term[ZhCN], want, text)
					}
				}
			}
		}
	}
	return problems
}

// decodeTables reads {"lang": {"key": "text"}} and reports keys that appear
// twice in one table (encoding/json would silently keep the last).
func decodeTables(b []byte) (map[string]map[string]string, []string, error) {
	var tables map[string]map[string]string
	if e := json.Unmarshal(b, &tables); e != nil {
		return nil, nil, e
	}
	var raw map[string]json.RawMessage
	_ = json.Unmarshal(b, &raw)
	var dups []string
	for lang, body := range raw {
		d := json.NewDecoder(bytes.NewReader(body))
		seen := map[string]bool{}
		if _, e := d.Token(); e != nil {
			continue
		}
		for d.More() {
			tok, e := d.Token()
			if e != nil {
				break
			}
			key, _ := tok.(string)
			if seen[key] {
				dups = append(dups, fmt.Sprintf("%s: %q appears twice", lang, key))
			}
			seen[key] = true
			var skip json.RawMessage
			if d.Decode(&skip) != nil {
				break
			}
		}
	}
	sort.Strings(dups)
	return tables, dups, nil
}

func placeholders(s string) []string {
	var out []string
	for _, m := range placeholder.FindAllStringSubmatch(s, -1) {
		out = append(out, m[1])
	}
	sort.Strings(out)
	return slices.Compact(out)
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
