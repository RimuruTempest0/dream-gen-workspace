package i18n

import (
	_ "embed"
	"encoding/json"
)

//go:embed glossary.json
var glossaryJSON []byte

// Term is one fixed term with its translation in every interface language.
type Term map[string]string

// Glossary returns the fixed-term glossary (glossary.json). Longer terms that
// contain shorter ones (心境引导, 心境) are both listed.
func Glossary() []Term {
	var g struct {
		Terms []Term `json:"terms"`
	}
	if e := json.Unmarshal(glossaryJSON, &g); e != nil {
		panic("i18n: glossary.json: " + e.Error())
	}
	return g.Terms
}

// Translate returns the glossary translation of a Chinese term, or "" when
// the term is not in the glossary.
func Translate(zh, lang string) string {
	for _, t := range Glossary() {
		if t[ZhCN] == zh {
			return t[lang]
		}
	}
	return ""
}
