package core

import (
	"strings"
	"testing"
	"unicode"

	"imageworkbench/internal/i18n"
)

// Feeling words the mood guide's options must never use: the customer points
// at a metaphor (weather, time of day, distance), not at a feeling.
var feelingWords = map[string][]string{
	i18n.ZhCN: {"情绪", "心情", "心境", "开心", "快乐", "高兴", "难过", "悲伤", "伤心", "孤独", "寂寞", "焦虑", "不安", "愤怒", "生气", "平静", "治愈", "希望", "忧郁", "抑郁", "烦"},
	i18n.En:   {"feel", "mood", "emotion", "happy", "joy", "sad", "lonely", "anxious", "anxiety", "angry", "calm", "peace", "heal", "hope", "gloom", "melanchol", "depress", "upset", "cheer", "worr", "stress"},
	i18n.Vi:   {"cảm xúc", "tâm trạng", "vui", "buồn", "cô đơn", "lo âu", "lo lắng", "giận", "bình yên", "hạnh phúc", "chữa lành", "u sầu"},
	i18n.Ja:   {"気持ち", "感情", "心境", "気分", "嬉し", "うれし", "楽し", "悲し", "寂し", "孤独", "不安", "怒", "穏やか", "癒", "憂"},
	i18n.Ko:   {"감정", "기분", "심경", "기쁘", "기쁨", "행복", "슬프", "슬픔", "외로", "불안", "화가", "평온", "위로", "치유", "우울"},
}

func hasHan(v string) bool {
	return strings.ContainsFunc(v, func(r rune) bool { return unicode.Is(unicode.Han, r) })
}

// Every option, concept, group and pairing has its own text in all five
// languages: nothing falls back to Chinese, and en/vi/ko carry no Han characters.
func TestFixedListsAreTranslated(t *testing.T) {
	for _, lang := range i18n.Languages {
		if lang == i18n.ZhCN {
			continue
		}
		texts, ok := fixedLists[lang]
		if !ok {
			t.Errorf("fixed_lists.json has no %s", lang)
			continue
		}
		noHan := lang != i18n.Ja
		check := func(what, v string) {
			if strings.TrimSpace(v) == "" {
				t.Errorf("%s %s: empty", lang, what)
			}
			if noHan && hasHan(v) {
				t.Errorf("%s %s: contains Chinese: %q", lang, what, v)
			}
		}
		ids := map[string]bool{}
		for _, list := range [][]MoodOption{moodWeather, moodTime, moodDistance, moodExpectation, moodRenderings, moodNudges} {
			for _, o := range list {
				ids[o.ID] = true
				tr, ok := texts.Moods[o.ID]
				if !ok {
					t.Errorf("%s: mood option %s missing", lang, o.ID)
					continue
				}
				check(o.ID+" name", tr.Name)
				check(o.ID+" short", tr.Short)
				if o.Phrase != "" {
					check(o.ID+" phrase", tr.Phrase)
				}
			}
		}
		for id := range texts.Moods {
			if !ids[id] {
				t.Errorf("%s: unknown mood option %s", lang, id)
			}
		}
		check("moodSentence", texts.MoodSentence)
		for _, p := range []string{"{time}", "{weather}", "{distance}", "{expectation}"} {
			if strings.Count(texts.MoodSentence, p) != 1 {
				t.Errorf("%s moodSentence must hold %s once", lang, p)
			}
		}
		groups := map[string]bool{}
		concepts := map[string]bool{}
		for _, c := range append(append([]Concept{}, conceptForms...), conceptStances...) {
			concepts[c.ID], groups[c.Group] = true, true
			tr, ok := texts.Concepts[c.ID]
			if !ok {
				t.Errorf("%s: concept %s missing", lang, c.ID)
				continue
			}
			check(c.ID+" name", tr.Name)
			check(c.ID+" look", tr.Look)
		}
		for id := range texts.Concepts {
			if !concepts[id] {
				t.Errorf("%s: unknown concept %s", lang, id)
			}
		}
		for g := range groups {
			check("group "+g, texts.Groups[g])
		}
		for _, p := range conceptPicks {
			check("pick "+p.Name, texts.Picks[p.Form+"/"+p.Stance])
		}
		if len(texts.Groups) != len(groups) || len(texts.Picks) != len(conceptPicks) {
			t.Errorf("%s: extra groups or picks", lang)
		}
	}
}

// The mood guide's options are metaphors in every language, never feeling words.
func TestMoodOptionsHaveNoFeelingWords(t *testing.T) {
	for _, lang := range i18n.Languages {
		lib := moodLibraryIn(lang)
		words := feelingWords[lang]
		if len(words) == 0 {
			t.Fatalf("no feeling words for %s", lang)
		}
		for _, list := range [][]MoodOption{lib.Weather, lib.Time, lib.Distance, lib.Expectation, lib.Nudges} {
			for _, o := range list {
				for _, v := range []string{o.Name, o.Short, o.Phrase} {
					low := strings.ToLower(v)
					for _, w := range words {
						if strings.Contains(low, w) {
							t.Errorf("%s %s: %q contains the feeling word %q", lang, o.ID, v, w)
						}
					}
				}
			}
		}
	}
}

// The library follows the interface language; stored labels keep the language
// they were written in.
func TestFixedListsFollowInterfaceLanguage(t *testing.T) {
	s := testService(t)
	if lib := s.ListMoods(); lib.Weather[3].Name != "细雨" || lib.Sentence != moodSentenceZh {
		t.Fatal("zh library changed", lib.Weather[3], lib.Sentence)
	}
	if e := s.SetLanguage(i18n.En); e != nil {
		t.Fatal(e)
	}
	lib := s.ListMoods()
	if lib.Weather[3].Name != "Drizzle" || lib.Nudges[0].Name != "Quieter" || !strings.Contains(lib.Sentence, "{weather}") || lib.Weather[3].Rules == "" {
		t.Fatal("mood library not in English", lib.Weather[3], lib.Nudges[0], lib.Sentence)
	}
	concepts := s.ListConcepts()
	if concepts.Forms[1].Name != "Megastructure" || concepts.Forms[1].Group != "Heavy" || concepts.Stances[0].Name != "Romanticism" || concepts.Picks[0].Name != "The sublime" || hasHan(concepts.Forms[1].Look) {
		t.Fatal("concept library not in English", concepts.Forms[1], concepts.Stances[0], concepts.Picks[0])
	}
	// The prompt side stays untouched: the base tables keep the Chinese names.
	if conceptForms[1].Name != "巨物" || moodWeather[3].Name != "细雨" {
		t.Fatal("base tables were modified")
	}
	m := MoodChoice{"drizzle", "night", "far", "soothe"}
	if got := moodLabelIn(m, i18n.En); got != "Drizzle · Night · Far · Catch my breath" {
		t.Fatal(got)
	}
	if got := moodLabelIn(m, i18n.Ko); got != "가랑비 · 깊은 밤 · 멀리 · 숨 돌리기" {
		t.Fatal(got)
	}
	if got := moodLabelIn(m, "de"); got != "细雨 · 深夜 · 很远 · 缓一缓" {
		t.Fatal("unknown language must fall back to zh-CN:", got)
	}
	form, _ := findConcept(conceptForms, "mega")
	stance, _ := findConcept(conceptStances, "wabisabi")
	if got := conceptLabelIn(form, stance, i18n.Ja); got != "巨大構造 × 侘び寂び" {
		t.Fatal(got)
	}
}

// A mood image submitted in English is labelled in English and keeps that label
// after the customer switches back to Chinese.
func TestMoodTaskLabelKeepsItsLanguage(t *testing.T) {
	s := testService(t)
	up, _, _ := moodRelay(t)
	c := saveConnection(t, s, up.URL)
	if e := s.SetLanguage(i18n.En); e != nil {
		t.Fatal(e)
	}
	if _, e := s.Submit(moodInput(c)); e != nil {
		t.Fatal(e)
	}
	if e := s.SetLanguage(i18n.ZhCN); e != nil {
		t.Fatal(e)
	}
	tasks, _ := List[Task](s.store, "task")
	if len(tasks) == 0 {
		t.Fatal("no task")
	}
	for _, x := range tasks {
		if x.Mood != "Drizzle · Night · Far · Lift me up" {
			t.Fatalf("label %q", x.Mood)
		}
	}
}
