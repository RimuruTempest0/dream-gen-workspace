package core

// p004FixedRules is appended to every image prompt of a run that uses the shipped
// p004-rules skill, so the image model receives the project rules even when the
// planning LLM leaves one out. It mirrors the locked sentences in that skill.
const p004FixedRules = "项目固定规则（P004）：画面中的人物模特（含成人、儿童和婴儿）均为欧美人；所有刺绣图案实际尺寸长边和短边均不超过 25 cm，同一图案在每张图中的实际尺寸和相对衣服的比例保持一致；卫衣胸前主刺绣整体以卫衣本体的垂直中轴线左右居中，以服装本体中心为准，不以图片中心为准；袖口刺绣只在最终画面右侧那只袖子的袖口，文字基线与该袖口接缝线平行并随袖口角度倾斜，整组位于袖筒宽度中心、接缝上方的平整处，不压罗纹或褶皱，画面左侧的袖子没有刺绣；袖口小图标与名字的左右顺序在每张图中完全相同；整张图片清晰，背景同样清晰，不使用任何虚化、柔焦、景深或运动模糊。"

// projectRulesSuffix returns the fixed rules for runs whose frozen skills include
// the unedited shipped p004-rules. An edited copy is left to speak for itself, so
// the backend never appends rules the user has changed.
func projectRulesSuffix(r AgentRun) string {
	for _, skill := range r.Skills {
		if skill.ID != p004SkillID {
			continue
		}
		for _, shipped := range shippedP004Contents() {
			if skill.Content == shipped {
				return "\n" + p004FixedRules
			}
		}
	}
	return ""
}

// shippedP004Contents lists every released p004-rules text, so a run frozen with
// an earlier unedited copy still gets the fixed rules after an upgrade.
func shippedP004Contents() []string {
	b := builtinSkills[3]
	return []string{
		builtinSkillContent(b.id, b.description, b.body),
		builtinSkillContent(b.id, b.description, embeddedSkillText("builtin_skills/history/p004-rules-0.3.6.md")),
	}
}
