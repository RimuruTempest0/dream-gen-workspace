package i18n

import (
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"unicode"
)

// The customer program's interface code (the admin tool keeps its Chinese
// interface and lives in src/admin).
const frontendDir = "../../frontend/src"

// frontendChineseAllowed lists, per file (relative to frontend/src), the
// exact pieces of code that may hold Chinese. Every interface text comes from
// the text tables in src/i18n/messages, so any other Chinese in the code fails
// TestNoChineseInFrontend. Only what is not interface text belongs here: names
// shown in their own script, font names, stored codes and patterns that
// recognise text written before 0.8.0 or by upstream services.
var frontendChineseAllowed = map[string]map[string]string{
	"AgentSettings.tsx": {
		`内置: "agentSettings.skill.source.builtin"`:   "stored skill source code (translated here)",
		`导入: "agentSettings.skill.source.imported"`:  "stored skill source code (translated here)",
		`自定义: "agentSettings.skill.source.custom"`:   "stored skill source code (translated here)",
	},
	"i18n/languages.ts": {
		`native: "中文"`:  "the language's own name in the language switch",
		`native: "日本語"`: "the language's own name in the language switch",
		`"思源宋体"`:       "Windows font family name",
	},
	"tokens.ts": {`"思源宋体"`: "Windows font family name"},
	"SuiteFeedback.tsx": {`"反馈.md": "feedback.preview.md"`: "file name in the 反馈包 (the file is Chinese for the provider)"},
	"SmartParts.tsx": {
		`正在用浏览器读取商品页: "browser-reading"`:    "stage text saved before 0.8.0",
		`需要你在弹出的窗口里完成验证: "browser-verify"`: "stage text saved before 0.8.0",
		`准备素材: "preparing"`:                  "stage text saved before 0.8.0",
	},
	"ui.tsx": {
		`"样板图："`:                 "recognises the Chinese sample line of an image prompt",
		`/^用途[：:]\s*/`:           "recognises the Chinese purpose line of an image prompt",
		`余额|额度|欠费|充值`:            "recognises upstream balance errors",
		`"图片服务账户余额不足"`:           "recognises task errors saved before 0.8.0",
		`"中转站账户余额不足"`:            "recognises task errors saved before 0.8.0",
		`"API Key 无效"`:            "recognises task errors saved before 0.8.0",
		`"生图模型的内容审核拒绝了这张图"`:      "recognises task errors saved before 0.8.0",
		`/^(?:(图片线路)|LLM 线路) (\d+)$/`: "default line names the admin tool writes into the license file (shown translated)",
	},
}

// TestNoChineseInFrontend reads the interface code outside the text tables:
// Chinese in code (comments aside) that frontendChineseAllowed does not list
// is hard-coded interface text.
func TestNoChineseInFrontend(t *testing.T) {
	used := map[string]bool{}
	files := 0
	e := filepath.WalkDir(frontendDir, func(path string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		rel, _ := filepath.Rel(frontendDir, path)
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if rel == "admin" || rel == "i18n/messages" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(rel, ".ts") && !strings.HasSuffix(rel, ".tsx") {
			return nil
		}
		b, e := os.ReadFile(path)
		if e != nil {
			return e
		}
		files++
		for _, p := range chineseInCode(rel, string(b), frontendChineseAllowed[rel], used) {
			t.Error(p)
		}
		return nil
	})
	if e != nil || files < 20 {
		t.Fatal("frontend sources not found in", frontendDir, files, e)
	}
	for file, pieces := range frontendChineseAllowed {
		for piece := range pieces {
			if !used[file+"\x00"+piece] {
				t.Errorf("frontendChineseAllowed[%q][%q] is not in the code any more; remove it", file, piece)
			}
		}
	}
}

// The checker must find Chinese in strings, template literals and JSX text,
// skip comments, and accept only the listed pieces.
func TestFrontendChineseCheckerCatchesMistakes(t *testing.T) {
	src := strings.Join([]string{
		`// 注释里的中文不算`,
		`/* 块注释
   也不算 */`,
		`const a = "写死的文字";`,
		"const b = `模板 ${x ? '里的' : \"\"} 文字`;",
		`const c = <p>标签里的文字</p>;`,
		`const d = {/* JSX 注释 */};`,
		`const r = /(余额)/.test(s) ? 1 : 2; const e = "允许的";`,
		`const u = "http://x"; // 行尾注释`,
	}, "\n")
	used := map[string]bool{}
	got := chineseInCode("x.tsx", src, map[string]string{`"允许的"`: "test"}, used)
	want := []string{
		`x.tsx:4: Chinese outside the text tables: const a = "写死的文字";`,
		"x.tsx:5: Chinese outside the text tables: const b = `模板 ${x ? '里的' : \"\"} 文字`;",
		`x.tsx:6: Chinese outside the text tables: const c = <p>标签里的文字</p>;`,
		`x.tsx:8: Chinese outside the text tables: const r = /(余额)/.test(s) ? 1 : 2; const e = "允许的";`,
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("got\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if !used["x.tsx\x00\"允许的\""] {
		t.Fatal("allowed piece not marked as used")
	}
}

// chineseInCode reports each line whose code (comments blanked) holds a Han
// character not inside one of the allowed pieces; used records the pieces seen.
func chineseInCode(file, src string, allowed map[string]string, used map[string]bool) []string {
	var problems []string
	for i, line := range strings.Split(stripComments(src), "\n") {
		covered := make([]bool, len(line))
		for piece := range allowed {
			for from := 0; ; {
				at := strings.Index(line[from:], piece)
				if at < 0 {
					break
				}
				at += from
				for j := at; j < at+len(piece); j++ {
					covered[j] = true
				}
				used[file+"\x00"+piece] = true
				from = at + len(piece)
			}
		}
		for j, r := range line {
			if unicode.Is(unicode.Han, r) && !covered[j] {
				problems = append(problems, file+":"+strconv.Itoa(i+1)+": Chinese outside the text tables: "+strings.TrimSpace(line))
				break
			}
		}
	}
	return problems
}


// stripComments blanks the comments of TypeScript/TSX source (keeping line
// breaks), leaving strings, template literals, regular expressions and JSX
// text in place.
func stripComments(src string) string {
	b := []rune(src)
	out := make([]rune, len(b))
	copy(out, b)
	blank := func(from, to int) {
		for k := from; k < to && k < len(out); k++ {
			if out[k] != '\n' {
				out[k] = ' '
			}
		}
	}
	// prev is the last significant character of code, to tell a regular
	// expression from a division.
	prev := rune(0)
	// Each template literal open records the brace depth of its ${ … }.
	var templates []int
	depth := 0
	i := 0
	for i < len(b) {
		c := b[i]
		next := rune(0)
		if i+1 < len(b) {
			next = b[i+1]
		}
		switch {
		case c == '/' && next == '/':
			j := i
			for j < len(b) && b[j] != '\n' {
				j++
			}
			blank(i, j)
			i = j
			continue
		case c == '/' && next == '*':
			j := i + 2
			for j+1 < len(b) && !(b[j] == '*' && b[j+1] == '/') {
				j++
			}
			blank(i, j+2)
			i = j + 2
			continue
		case c == '"' || c == '\'':
			j := i + 1
			for j < len(b) && b[j] != c && b[j] != '\n' {
				if b[j] == '\\' {
					j++
				}
				j++
			}
			i = j + 1
			prev = 'a'
			continue
		case c == '`':
			i = skipTemplate(b, i+1, &templates, depth)
			prev = 'a'
			continue
		case c == '}' && len(templates) > 0 && templates[len(templates)-1] == depth:
			templates = templates[:len(templates)-1]
			i = skipTemplate(b, i+1, &templates, depth)
			prev = 'a'
			continue
		case c == '/' && (prev == 0 || strings.ContainsRune("(,=:[!&|?{};+-*%<>~^", prev)):
			j := i + 1
			inClass := false
			for j < len(b) && b[j] != '\n' && (inClass || b[j] != '/') {
				switch b[j] {
				case '\\':
					j++
				case '[':
					inClass = true
				case ']':
					inClass = false
				}
				j++
			}
			i = j + 1
			prev = 'a'
			continue
		case c == '{':
			depth++
		case c == '}':
			depth--
		}
		if !unicode.IsSpace(c) {
			prev = c
		}
		i++
	}
	return string(out)
}

// skipTemplate moves past template-literal text from i, up to its closing
// backtick or the start of a ${ … } expression (whose brace depth it records).
func skipTemplate(b []rune, i int, templates *[]int, depth int) int {
	for i < len(b) {
		switch {
		case b[i] == '\\':
			i += 2
			continue
		case b[i] == '`':
			return i + 1
		case b[i] == '$' && i+1 < len(b) && b[i+1] == '{':
			*templates = append(*templates, depth)
			return i + 2
		}
		i++
	}
	return i
}
