package core

import (
	"encoding/json"
	"os"
	"testing"
)

// Settings shows a built-in skill's name and description in the display
// language while they are unchanged, by comparing them with the zh-CN text of
// agentSettings.builtin.<id>.* in the interface's text table. When a built-in
// skill's name or description changes here, update that table (all five
// languages) too, or the skill shows in Chinese in every language.
func TestBuiltinSkillTextsMatchTheInterfaceTable(t *testing.T) {
	b, e := os.ReadFile("../../frontend/src/i18n/messages/agentSettings.json")
	if e != nil {
		t.Fatal(e)
	}
	var tables map[string]map[string]string
	if e := json.Unmarshal(b, &tables); e != nil {
		t.Fatal(e)
	}
	zh := tables["zh-CN"]
	for _, s := range builtinSkills {
		for field, want := range map[string]string{"name": s.name, "description": s.description} {
			key := "agentSettings.builtin." + s.id + "." + field
			if got, ok := zh[key]; !ok {
				t.Errorf("%s is missing from the interface text table", key)
			} else if got != want {
				t.Errorf("%s = %q, the built-in skill has %q", key, got, want)
			}
		}
	}
}
