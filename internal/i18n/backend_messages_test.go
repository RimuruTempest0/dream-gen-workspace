package i18n

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// TestBackendCatalogComplete checks the backend message catalogs with the same
// rules as the frontend text tables (five languages, same keys and
// placeholders, no leftover Chinese, glossary terms).
func TestBackendCatalogComplete(t *testing.T) {
	paths, e := filepath.Glob("messages/*.json")
	if e != nil || len(paths) == 0 {
		t.Fatal("no backend catalogs found", e)
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

// messageCallers are the helpers that take a catalog key, with the position
// of the key argument; name/value pairs follow it.
var messageCallers = map[string]int{"msg": 0, "fail": 0, "msgCtx": 1, "failCtx": 1, "T": 1}

// backendSources are the Go packages whose messages come from the catalogs.
var backendSources = []string{"../core", "../.."}

// TestBackendMessageCalls reads the Go sources: every message call names a
// key in the catalog with exactly its placeholders, and every catalog key is
// used somewhere.
func TestBackendMessageCalls(t *testing.T) {
	zh := messages()[ZhCN]
	literals := map[string]bool{}
	calls := 0
	for _, dir := range backendSources {
		paths, _ := filepath.Glob(filepath.Join(dir, "*.go"))
		for _, path := range paths {
			if strings.HasSuffix(path, "_test.go") {
				continue
			}
			fset := token.NewFileSet()
			f, e := parser.ParseFile(fset, path, nil, 0)
			if e != nil {
				t.Fatal(e)
			}
			ast.Inspect(f, func(n ast.Node) bool {
				if lit, ok := n.(*ast.BasicLit); ok && lit.Kind == token.STRING {
					if v, e := strconv.Unquote(lit.Value); e == nil {
						literals[v] = true
					}
				}
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				name := ""
				switch fn := call.Fun.(type) {
				case *ast.Ident:
					name = fn.Name
				case *ast.SelectorExpr:
					name = fn.Sel.Name
				}
				at, ok := messageCallers[name]
				if !ok || len(call.Args) <= at {
					return true
				}
				lit, ok := call.Args[at].(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					return true // a key held in a variable or map: checked by the "used" rule
				}
				key, _ := strconv.Unquote(lit.Value)
				where := fset.Position(call.Pos()).String()
				text, known := zh[key]
				if !known {
					t.Errorf("%s: unknown message key %q", where, key)
					return true
				}
				calls++
				rest := call.Args[at+1:]
				if len(rest)%2 != 0 {
					t.Errorf("%s: %q: arguments must be name, value pairs", where, key)
					return true
				}
				var names []string
				for i := 0; i < len(rest); i += 2 {
					l, ok := rest[i].(*ast.BasicLit)
					if !ok || l.Kind != token.STRING {
						t.Errorf("%s: %q: placeholder names must be string literals", where, key)
						return true
					}
					v, _ := strconv.Unquote(l.Value)
					names = append(names, v)
				}
				sort.Strings(names)
				if want := placeholders(text); !slices.Equal(names, want) {
					t.Errorf("%s: %q passes %v, the text has %v", where, key, names, want)
				}
				return true
			})
		}
	}
	if calls == 0 {
		t.Fatal("no message calls found; is backendSources right?")
	}
	for _, key := range sortedKeys(zh) {
		if !literals[key] {
			t.Errorf("catalog key %q is not used in the Go sources", key)
		}
	}
}

func TestTFillsPlaceholdersAndFallsBack(t *testing.T) {
	if got := T(En, "task.log.concurrency", "n", 3); got != "Images at once set to 3" {
		t.Error(got)
	}
	if got := T("xx", "task.log.concurrency", "n", 3); got != "同时生成张数调整为 3" {
		t.Error("unknown language must fall back to zh-CN:", got)
	}
	if got := T(En, "no.such.key"); got != "no.such.key" {
		t.Error(got)
	}
	// A value is inserted as is, even when it looks like a placeholder.
	if got := T(En, "connection.models.failed", "error", "{error} 原文"); !strings.Contains(got, "{error} 原文") {
		t.Error(got)
	}
	if !HasPrefix("The image model's content check refused this image (HTTP 400)", "generation.policyRefused") || HasPrefix("other", "generation.policyRefused") {
		t.Error("HasPrefix")
	}
}
