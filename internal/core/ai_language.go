package core

import (
	"fmt"
	"strings"

	"imageworkbench/internal/i18n"
)

// ---- 对话语言与生图提示词 (0.8.0) ---------------------------------------------
//
// Every request that asks the AI 模型 for text the customer reads (questions,
// reasons, explanations, reports) names the 对话语言, which is the interface
// language. Every request that asks it to write a 生图提示词 names the prompt
// language: Chinese for a Chinese interface, English for the other four. What
// the customer typed may be in any language and is not translated beforehand;
// in-image text (names, slogans) is drawn exactly as written, never translated.
//
// The language is taken when an operation starts (Task.Language,
// AgentRun.Language, or at the start of a synchronous call) and passed down, so
// an operation already under way finishes in the language it began in, and the
// next one uses the language chosen since. Fixed lists (mood guide options,
// 形体/立场 names) are translated by the software, not by the AI 模型; a
// per-language lookup for them takes the same captured code.

// operationLanguage is a captured language, or Chinese for work saved before
// 0.8.0 (it was started in Chinese).
func operationLanguage(lang string) string {
	if i18n.Supported(lang) {
		return lang
	}
	return i18n.Default
}

// promptLanguage is the 生图提示词 language for an interface language.
func promptLanguage(lang string) string {
	if i18n.IsChinese(operationLanguage(lang)) {
		return i18n.ZhCN
	}
	return i18n.En
}

// aiLanguageNames name each language for the AI 模型; the requests are written
// in Chinese, so the native name goes along to remove any doubt.
var aiLanguageNames = map[string]string{
	i18n.ZhCN: "中文",
	i18n.En:   "英文（English）",
	i18n.Vi:   "越南语（Tiếng Việt）",
	i18n.Ja:   "日语（日本語）",
	i18n.Ko:   "韩语（한국어）",
}

// inImageTextRule keeps the customer's in-image text as written.
const inImageTextRule = "画面里要出现的文字（名字、标语、字母和数字，尤其是引号里的）照用户写的原文逐字保留，连同引号一起写进提示词，不翻译、不音译、不改大小写。"

// promptLanguageRule is the line that sets the language of a 生图提示词.
func promptLanguageRule(lang string) string {
	if promptLanguage(lang) == i18n.ZhCN {
		return "语言：生图提示词用中文写；用户的提示词是其他语言时，把画面描述写成中文。" + inImageTextRule
	}
	return "语言：生图提示词用英文（English）写；用户的提示词是其他语言时，把画面描述写成英文。" + inImageTextRule
}

// replyLanguageRule is the line that sets the 对话语言 of text the customer reads.
func replyLanguageRule(lang string) string {
	return fmt.Sprintf("对话语言：写给用户看的文字（提问、选项、判断理由、说明和报告）一律用%s写。", aiLanguageNames[operationLanguage(lang)])
}

// agentLanguageRule is the language part of the planning Agent's prompt: the
// 对话语言 for questions and reports, the prompt language for save_suite_plan.
// It decides over a skill that says to write in Chinese.
func agentLanguageRule(lang string) string {
	return replyLanguageRule(lang) + "save_suite_plan 里的 prompt 是生图提示词，" +
		strings.TrimPrefix(promptLanguageRule(lang), "语言：生图提示词") +
		"技能里关于用中文写提示词或与用户沟通的说法，以这里的语言为准。"
}

// withPromptLanguage puts the prompt-language line ahead of a rewriting request.
func withPromptLanguage(lang, request string) string {
	return promptLanguageRule(lang) + "\n\n" + request
}
