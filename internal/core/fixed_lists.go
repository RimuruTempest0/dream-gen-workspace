package core

// ---- fixed lists in the conversation language (0.8.0) ---------------------------
//
// The mood guide's options, the renderings, the 再靠近一点 directions and the
// concept stage's forms, stances, groups and pairings are built in. Their
// customer-facing names are translated here, not by the AI on the fly (spec
// #1): zh-CN is the source in moods.go and concepts.go, the other languages
// come from fixed_lists.json. Only names, labels and the confirmation sentence
// are translated; the Rules and Hints sent to the AI stay as they are.
//
// Lookup: moodIn / conceptIn / conceptGroupIn / conceptPickIn / moodSentenceIn
// take an option (or key) and a language code; moodLibraryIn / conceptLibraryIn
// give a whole list. An unknown language or a missing entry falls back to zh-CN.

import (
	_ "embed"
	"encoding/json"
	"strings"

)

//go:embed fixed_lists.json
var fixedListsJSON []byte

type fixedMoodText struct {
	Name   string `json:"name"`
	Short  string `json:"short"`
	Phrase string `json:"phrase"`
}

type fixedConceptText struct {
	Name string `json:"name"`
	Look string `json:"look"`
}

type fixedListTexts struct {
	Moods        map[string]fixedMoodText    `json:"moods"`
	MoodSentence string                      `json:"moodSentence"`
	Concepts     map[string]fixedConceptText `json:"concepts"`
	Groups       map[string]string           `json:"groups"`
	Picks        map[string]string           `json:"picks"`
}

// fixedLists holds the non-Chinese texts by language code.
var fixedLists = func() map[string]fixedListTexts {
	var v map[string]json.RawMessage
	if e := json.Unmarshal(fixedListsJSON, &v); e != nil {
		panic("fixed_lists.json: " + e.Error())
	}
	out := map[string]fixedListTexts{}
	for lang, raw := range v {
		if lang == "about" {
			continue
		}
		var t fixedListTexts
		if e := json.Unmarshal(raw, &t); e != nil {
			panic("fixed_lists.json " + lang + ": " + e.Error())
		}
		out[lang] = t
	}
	return out
}()

// moodSentenceZh is the confirmation sentence of the mood guide; the frontend
// fills {time} {weather} {distance} {expectation} with the options' phrases.
const moodSentenceZh = "此刻像一个{time}，{weather}。你想{distance}，让这张图{expectation}。"

// moodIn returns o with its name, short label and phrase in lang.
func moodIn(o MoodOption, lang string) MoodOption {
	if t, ok := fixedLists[lang].Moods[o.ID]; ok {
		o.Name, o.Short = orZh(t.Name, o.Name), orZh(t.Short, o.Short)
		o.Phrase = orZh(t.Phrase, o.Phrase)
	}
	return o
}

func moodsIn(list []MoodOption, lang string) []MoodOption {
	out := make([]MoodOption, len(list))
	for i, o := range list {
		out[i] = moodIn(o, lang)
	}
	return out
}

// moodSentenceIn is the confirmation-sentence template in lang.
func moodSentenceIn(lang string) string {
	return orZh(fixedLists[lang].MoodSentence, moodSentenceZh)
}

// moodLibraryIn is the whole mood library in lang.
func moodLibraryIn(lang string) MoodLibrary {
	return MoodLibrary{
		Weather:     moodsIn(moodWeather, lang),
		Time:        moodsIn(moodTime, lang),
		Distance:    moodsIn(moodDistance, lang),
		Expectation: moodsIn(moodExpectation, lang),
		Renderings:  moodsIn(moodRenderings, lang),
		Nudges:      moodsIn(moodNudges, lang),
		Sentence:    moodSentenceIn(lang),
	}
}

// conceptIn returns c with its name, look and group in lang. En (the card's
// English subtitle, also used in the AI request) is kept.
func conceptIn(c Concept, lang string) Concept {
	if t, ok := fixedLists[lang].Concepts[c.ID]; ok {
		c.Name, c.Look = orZh(t.Name, c.Name), orZh(t.Look, c.Look)
	}
	c.Group = conceptGroupIn(c.Group, lang)
	return c
}

// conceptGroupIn translates a group name ("重", "感性" …).
func conceptGroupIn(group, lang string) string {
	return orZh(fixedLists[lang].Groups[group], group)
}

// conceptPickIn returns p with its name in lang.
func conceptPickIn(p ConceptPick, lang string) ConceptPick {
	p.Name = orZh(fixedLists[lang].Picks[p.Form+"/"+p.Stance], p.Name)
	return p
}

// conceptLibraryIn is the whole concept library in lang.
func conceptLibraryIn(lang string) ConceptLibrary {
	lib := ConceptLibrary{}
	for _, c := range conceptForms {
		lib.Forms = append(lib.Forms, conceptIn(c, lang))
	}
	for _, c := range conceptStances {
		lib.Stances = append(lib.Stances, conceptIn(c, lang))
	}
	for _, p := range conceptPicks {
		lib.Picks = append(lib.Picks, conceptPickIn(p, lang))
	}
	return lib
}

// conceptLabelIn is "巨物 × 浪漫主义" in lang, as shown on results.
func conceptLabelIn(form, stance Concept, lang string) string {
	return conceptIn(form, lang).Name + " × " + conceptIn(stance, lang).Name
}

// moodLabelIn is the short label shown on results, "细雨 · 深夜 · 很远 · 缓一缓", in lang.
func moodLabelIn(m MoodChoice, lang string) string {
	p, e := resolveMood(m)
	if e != nil {
		return ""
	}
	parts := []MoodOption{p.weather, p.time, p.distance, p.expectation}
	shorts := make([]string, len(parts))
	for i, o := range parts {
		shorts[i] = moodIn(o, lang).Short
	}
	return strings.Join(shorts, " · ")
}

func orZh(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}

