package i18n

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"unicode"
)

// chineseAllowed lists the declarations whose Go string literals may hold
// Chinese, by file and top-level declaration (a function or method name, or
// the first name of a var/const/type block). Everything else the backend
// returns to the interface comes from the catalogs in messages/*.json (zh-CN
// included), so a new Chinese literal elsewhere fails TestNoChineseInBackend.
// Only text that is not shown as interface text belongs here: requests and
// tool descriptions sent to the AI 模型, the zh-CN fixed lists (#5) and the
// Chinese image-prompt lines (their English twins are in prompt_scaffold.go),
// patterns that recognise upstream or saved text, file names and fixtures.
var chineseAllowed = map[string]map[string]string{
	"agent_bridge.go": {
		"agentTools": "tool descriptions for the planning Agent",
		"toolsFor":   "tool descriptions for the planning Agent",
		"callTool":   "tool results written for the Agent (errors come from the catalog)",
	},
	"agent_runner.go": {
		"executeAgent":    "the planning Agent's prompt",
		"agentSkillsText": "skills section of the Agent's prompt",
	},
	"agent_types.go": {"UnmarshalJSON": "recognises product notes saved before 0.6.7 (Chinese)"},
	"ai_language.go": {
		"agentLanguageRule":  "language rule sent to the AI",
		"aiLanguageNames":    "language names sent to the AI",
		"inImageTextRule":    "rule sent to the AI",
		"promptLanguageRule": "rule sent to the AI",
		"replyLanguageRule":  "rule sent to the AI",
	},
	"auto_mode.go": {
		"answerQuestions":     "tool result for the Agent",
		"answerQuestionsTool": "tool description for the Agent",
		"askingStyle":         "Agent prompt",
		"autoContinueLocked":  "instruction for the Agent's next round",
		"autoModeRule":        "Agent prompt",
		"autoSelfAnswer":      "Agent prompt",
		"questionRule":        "Agent prompt",
	},
	"concepts.go": {
		"conceptFallback":    "Chinese image-prompt lines (English: enConceptFallback)",
		"conceptForms":       "zh-CN fixed list and its AI rules",
		"conceptStances":     "zh-CN fixed list and its AI rules",
		"conceptPicks":       "zh-CN fixed list",
		"conceptStrengths":   "strength wording sent to the AI",
		"conceptInstruction": "request sent to the AI",
		"conceptNoRefsNote":  "request sent to the AI",
		"conceptRefsNote":    "request sent to the AI",
		"conceptRequest":     "request sent to the AI",
	},
	"crop.go": {
		"cropReference":     "tool result note for the Agent",
		"cropReferenceTool": "tool description for the Agent",
	},
	"fixed_lists.go": {"moodSentenceZh": "zh-CN fixed text"},
	"gateways.go":    {"balanceWords": "recognises upstream error text", "quotaUsedUp": "recognises upstream error text"},
	"license_qa.go":  {"writeQACustomer": "desktop QA licence fixture (provider data)"},
	"llm_monitor.go": {"legacyWaitingStages": "recognises stage names saved before 0.8.0"},
	"moods.go": {
		"moodWeather":       "zh-CN fixed list and its AI rules",
		"moodTime":          "zh-CN fixed list and its AI rules",
		"moodDistance":      "zh-CN fixed list and its AI rules",
		"moodExpectation":   "zh-CN fixed list and its AI rules",
		"moodRenderings":    "zh-CN fixed list and its AI rules",
		"moodNudges":        "zh-CN fixed list and its AI rules",
		"moodFallback":      "Chinese image-prompt lines (English: enMoodFallback)",
		"moodInstruction":   "request sent to the AI",
		"moodNoRendering":   "request sent to the AI",
		"moodRequest":       "request sent to the AI",
		"nudgePrompt":       "Chinese image prompt (English: nudgePromptIn)",
		"nudgePromptMarker": "Chinese image prompt marker",
	},
	"policy_retry.go": {
		"policyWords":       "recognises upstream error text",
		"rewordInstruction": "request sent to the AI",
	},
	"project_rules.go": {
		"itemPrompt":      "Chinese image-prompt lines (English: prompt_scaffold.go)",
		"p004RulesPrefix": "Chinese image-prompt lines (English: prompt_scaffold.go)",
		"p004TiltWording": "recognises prompts saved before 0.6.13",
		"sampleLead":      "Chinese image-prompt lines (English: prompt_scaffold.go)",
		"sampleSelfLine":  "Chinese image-prompt line (English: prompt_scaffold.go)",
	},
	"prompt_scaffold.go": {"samplePrefixes": "recognises saved Chinese sample lines"},
	"refine.go": {
		"refineMergeInstruction": "request sent to the AI",
		"refinePrompt":           "Chinese image-prompt lines (English: prompt_scaffold.go)",
		"refinePromptMarker":     "Chinese image-prompt marker",
		"refinePromptMarkerV1":   "recognises prompts saved before 0.6.11",
		"refineSampleLine":       "Chinese image-prompt line",
		"refineSamplesLine":      "Chinese image-prompt line",
		"removalWords":           "recognises the customer's words",
	},
	"sample.go": {
		"samplePromptLine":  "Chinese image-prompt line (English: prompt_scaffold.go)",
		"samplesPromptLine": "Chinese image-prompt line (English: prompt_scaffold.go)",
	},
	"sample_check.go": {
		"maxSampleRedraws":       "Chinese sample-fix marker of the image prompt",
		"sampleCheckInstruction": "request sent to the AI",
	},
	"shop_rules.go": {"shopRulesText": "Agent prompt"},
	"skills.go": {
		"builtinSkills":                "built-in skill content written for the Agent",
		"etsySuiteBodyV1":              "earlier built-in skill content (recognised for upgrades)",
		"productAnalysisDescriptionV1": "earlier built-in skill content (recognised for upgrades)",
		"ImportSkill":                  "stored skill source code (内置/自定义/导入), translated by the interface",
		"SaveSkill":                    "stored skill source code, translated by the interface",
		"putBuiltinSkill":              "stored skill source code, translated by the interface",
		"upgradeBuiltinSkills":         "stored skill source code, translated by the interface",
	},
	"update.go":    {"Manuals": "file names of the 使用说明"},
	"update_qa.go": {"PrepareTestUpdate": "desktop QA update fixture"},
}

// TestNoChineseInBackend reads the backend sources: a string literal with
// Chinese outside chineseAllowed is interface text that must come from the
// catalogs instead.
func TestNoChineseInBackend(t *testing.T) {
	hasHan := func(s string) bool {
		return strings.ContainsFunc(s, func(r rune) bool { return unicode.Is(unicode.Han, r) })
	}
	used := map[string]bool{}
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
			file := filepath.Base(path)
			for _, d := range f.Decls {
				name := declName(d)
				ast.Inspect(d, func(n ast.Node) bool {
					lit, ok := n.(*ast.BasicLit)
					if !ok || lit.Kind != token.STRING {
						return true
					}
					v, _ := strconv.Unquote(lit.Value)
					if !hasHan(v) {
						return true
					}
					if _, ok := chineseAllowed[file][name]; ok {
						used[file+":"+name] = true
						return true
					}
					t.Errorf("%s (%s): Chinese text outside the catalogs: %.60q", fset.Position(lit.Pos()), name, v)
					return true
				})
			}
		}
	}
	for file, decls := range chineseAllowed {
		for name := range decls {
			if !used[file+":"+name] {
				t.Errorf("chineseAllowed[%q][%q] has no Chinese any more; remove it", file, name)
			}
		}
	}
}

func declName(d ast.Decl) string {
	switch d := d.(type) {
	case *ast.FuncDecl:
		return d.Name.Name
	case *ast.GenDecl:
		for _, s := range d.Specs {
			switch s := s.(type) {
			case *ast.ValueSpec:
				return s.Names[0].Name
			case *ast.TypeSpec:
				return s.Name.Name
			}
		}
	}
	return ""
}
