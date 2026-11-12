package core

import "strings"

// Versions 0.7.10–0.7.16 kept the text of unedited built-in skills out of customer
// data folders: the skill list and the frozen copies in runs held a short sealed
// stub and the text was read from the program. The skills are visible again (the
// user's decision), so a sealed copy is restored to the full current text when a
// data folder or a restored backup is opened. That is what a sealed copy already
// meant, so nothing a run does changes.

func builtinByID(id string) (builtinSkill, bool) {
	for _, b := range builtinSkills {
		if b.id == id {
			return b, true
		}
	}
	return builtinSkill{}, false
}

// unsealSkills restores sealed built-in skills in place and reports whether any was.
func unsealSkills(skills []SkillRecord) bool {
	changed := false
	for i, v := range skills {
		if !v.Sealed {
			continue
		}
		if b, ok := builtinByID(v.ID); ok {
			skills[i].Content = b.content()
		}
		skills[i].Sealed = false
		changed = true
	}
	return changed
}

func (s *Service) unsealBuiltinSkills() error {
	skills, e := List[SkillRecord](s.store, "skill")
	if e != nil {
		return e
	}
	for i := range skills {
		one := skills[i : i+1]
		if unsealSkills(one) {
			if e = s.store.Put("skill", one[0].ID, one[0]); e != nil {
				return e
			}
		}
	}
	runs, e := List[AgentRun](s.store, "agent_run")
	if e != nil {
		return e
	}
	for _, r := range runs {
		if unsealSkills(r.Skills) {
			if e = s.store.Put("agent_run", r.ID, r); e != nil {
				return e
			}
		}
	}
	return nil
}

// BuiltinSkill describes a shipped skill for the admin tool's selection.
type BuiltinSkill struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

func BuiltinSkills() []BuiltinSkill {
	out := []BuiltinSkill{}
	for _, b := range builtinSkills {
		out = append(out, BuiltinSkill{ID: b.id, Name: b.name, Description: b.description})
	}
	return out
}

// applySkillSelectionLocked switches built-in skills on or off to match the
// selection in customer.dat, once per change of that selection, so a customer's
// own later switching is kept. No selection (nil) means all, and changes nothing.
func (s *Service) applySkillSelectionLocked(rec *licenseRecord) error {
	p := s.lic.profile
	if p == nil || p.Skills == nil {
		return nil
	}
	key := strings.Join(p.Skills, ",")
	if rec.SkillsApplied == key {
		return nil
	}
	chosen := map[string]bool{}
	for _, id := range p.Skills {
		chosen[id] = true
	}
	for _, b := range builtinSkills {
		var v SkillRecord
		if s.store.Get("skill", b.id, &v) != nil || v.Enabled == chosen[b.id] {
			continue
		}
		v.Enabled = chosen[b.id]
		if e := s.store.Put("skill", v.ID, v); e != nil {
			return e
		}
	}
	rec.SkillsApplied = key
	return nil
}
