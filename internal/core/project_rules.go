package core

import (
	"fmt"
	"slices"
	"strings"
)

// The P004 fixed rules, split so the cuff clauses can be left out for products
// without cuff embroidery: telling the image model where cuff embroidery goes
// makes it invent some even when the plan says the cuffs are plain.
const (
	p004RulesPrefix  = "项目固定规则（P004）：画面中的人物模特（含成人、儿童和婴儿）均为欧美人；所有刺绣图案实际尺寸长边和短边均不超过 25 cm，同一图案在每张图中的实际尺寸和相对衣服的比例保持一致；卫衣胸前主刺绣整体以卫衣本体的垂直中轴线左右居中，以服装本体中心为准，不以图片中心为准；"
	p004CuffRules    = "袖口刺绣只在最终画面右侧那只袖子的袖口，这只袖子在画面里竖直向下、袖口接缝水平，文字基线与袖口接缝线平行，图标和文字在画面中水平摆正、不倾斜，整组位于袖筒宽度中心、接缝上方的平整处，不压罗纹或褶皱，画面左侧的袖子没有刺绣；袖口小图标与名字的左右顺序在每张图中完全相同；"
	p004NoCuffRules  = "两只袖子的袖口都没有任何刺绣、文字或图案；"
	p004ClarityRules = "整张图片清晰，背景同样清晰，不使用任何虚化、柔焦、景深或运动模糊。"
)

// p004TiltWording is how rules before 0.6.13 (the fixed clause and the locked
// sentence copied from the skill) described cuff text: tilted with the sleeve. It
// made cuff text diagonal; a refine replaces it so the old prompt does not argue
// against straightening the text.
const (
	p004TiltWording  = "并随袖口角度倾斜"
	p004LevelWording = "，图标和文字在画面中水平摆正、不倾斜"
)

// p004FixedRules is appended to every image prompt of a run that uses the shipped
// p004-rules skill, so the image model receives the project rules even when the
// planning LLM leaves one out. It mirrors the locked sentences in that skill.
const p004FixedRules = p004RulesPrefix + p004CuffRules + p004ClarityRules

// p004FixedRulesNoCuff replaces p004FixedRules when the plan states the product
// has no cuff embroidery.
const p004FixedRulesNoCuff = p004RulesPrefix + p004NoCuffRules + p004ClarityRules

// projectRulesSuffix returns the fixed rules for runs whose frozen skills include
// the unedited shipped p004-rules. An edited copy is left to speak for itself, so
// the backend never appends rules the user has changed. Plans saved before
// cuffEmbroidery existed keep the full rules.
func projectRulesSuffix(r AgentRun, p SuitePlan) string {
	for _, skill := range r.Skills {
		if skill.ID != p004SkillID {
			continue
		}
		for _, shipped := range shippedP004Contents() {
			if skill.Content == shipped {
				if p.CuffEmbroidery != nil && !*p.CuffEmbroidery {
					return "\n" + p004FixedRulesNoCuff
				}
				return "\n" + p004FixedRules
			}
		}
	}
	return ""
}

// usesP004 reports whether the run planned with any copy of p004-rules.
func usesP004(r AgentRun) bool {
	for _, skill := range r.Skills {
		if skill.ID == p004SkillID {
			return true
		}
	}
	return false
}

// shippedP004Contents lists every released p004-rules text, so a run frozen with
// an earlier unedited copy still gets the fixed rules after an upgrade.
func shippedP004Contents() []string {
	b := builtinSkills[3]
	return []string{
		b.content(),
		builtinSkillContent(b.id, b.description, embeddedSkillText("builtin_skills/history/p004-rules-0.3.6.md")),
		builtinSkillContent(b.id, b.description, embeddedSkillText("builtin_skills/history/p004-rules-0.5.1.md")),
		builtinSkillContent(b.id, b.description, embeddedSkillText("builtin_skills/history/p004-rules-0.6.10.md")),
		builtinSkillContent(b.id, b.description, embeddedSkillText("builtin_skills/history/p004-rules-0.6.12.md")),
		builtinSkillContent(b.id, b.description, embeddedSkillText("builtin_skills/history/p004-rules-0.6.20.md")),
		builtinSkillContent(b.id, b.description, embeddedSkillText("builtin_skills/history/p004-rules-0.6.25.md")),
	}
}

// itemPrompt is the prompt sent for one plan item. An image without the product
// (a thread-colour card, fabric swatches) gets neither the locked product spec nor
// the product rules: given the full garment description, the image model draws
// the garment, cuff embroidery included, even when told not to.
func itemPrompt(r AgentRun, p SuitePlan, item SuiteItem) string {
	suffix := projectRulesSuffix(r, p)
	if item.NoProduct {
		prompt := item.Prompt + "\n画面比例：" + r.Input.Ratio
		if suffix != "" {
			prompt += "\n项目固定规则（P004）：" + p004ClarityRules
		}
		return prompt
	}
	prompt := item.Prompt + "\n统一锁定项：" + strings.Join(p.Locked, "；") + "\n允许修改：" + strings.Join(p.Editable, "；") + "\n画面比例：" + r.Input.Ratio + suffix
	if item.Sample {
		prompt = sampleLead(item.SampleArea) + prompt
	}
	return prompt
}

// maxSamples is how many samples a set may have: one per design area.
const maxSamples = 3

// samplesUsed lists the plan IDs of the samples item is drawn from, in the order
// they become its first references: the areas it names, or every sample when it
// only sets useSample (plans before 0.7.9).
func samplesUsed(p SuitePlan, item SuiteItem) []string {
	if item.Sample {
		return nil
	}
	byArea, all := map[string]string{}, []string{}
	for _, x := range p.Items {
		if x.Sample {
			byArea[strings.TrimSpace(x.SampleArea)] = x.ID
			all = append(all, x.ID)
		}
	}
	if len(item.UseSamples) == 0 {
		if item.UseSample {
			return all
		}
		return nil
	}
	out := []string{}
	for _, area := range item.UseSamples {
		if id := byArea[strings.TrimSpace(area)]; id != "" && !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	return out
}

// sampleLead opens a sample's own prompt. A cuff-like detail sample keeps the
// close-up rule; the master of a main design area shows its placement and size.
func sampleLead(area string) string {
	if area == "" || strings.Contains(area, "袖") {
		return sampleSelfLine
	}
	return fmt.Sprintf("设计母版（本套其他图片会照这张画「%[1]s」的设计，必须画准）：按提示词的取景画；画面里「%[1]s」设计的图案、朝向、位置和相对大小就是全套的标准，必须清楚、完整、端正。\n", area)
}

// sampleSelfLine leads the sample's own prompt. With a whole-garment reference and
// the full locked spec, the model tends to draw the whole garment with the sleeve at
// an angle, and every image drawn from the sample copies that angle.
const sampleSelfLine = "细节样板（本套其他图片会照这张画，必须画准）：只拍这组细节所在的局部近景特写（例如袖口局部），袖子竖直、袖口接缝水平，图标和文字在画面中水平摆正、不倾斜；统一锁定项里不在这个局部里的内容（胸前图案、领口、整件衣服）都不要画出来，也不要照参考图画整件衣服。\n"
