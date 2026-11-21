package core

import (
	"fmt"
	"strings"

	"imageworkbench/internal/i18n"
)

// ---- fixed lines the backend adds to a 生图提示词 (0.8.0) ----------------------
//
// Smart generation wraps the prompts the AI wrote with fixed lines: the locked
// spec and what may change, the aspect ratio, the P004 project rules, sample
// leads, the refinement head and the corrections of a redrawn sample. They are
// part of the image prompt, which the customer sees on the card, so they follow
// the prompt language of the operation (promptLanguage: Chinese for a Chinese
// interface, English for the other four). The Chinese wording stays where it
// was (project_rules.go, sample.go, refine.go, sample_check.go); the English
// wording is here. Markers that are looked for in saved prompts are recognised
// in both languages.

// zhPrompt reports whether the operation's image prompts are written in Chinese.
func zhPrompt(lang string) bool { return promptLanguage(lang) == i18n.ZhCN }

const (
	enRatioLine    = "\nAspect ratio: "
	enLockedLine   = "\nLocked for the whole set: "
	enEditableLine = "\nMay change: "
	enListSep      = "; "
	enRulesLine    = "\nProject fixed rules (P004): "
)

// The P004 fixed rules in English, split like the Chinese ones.
const (
	enP004RulesPrefix  = "Project fixed rules (P004): every human model in the picture (adults, children and babies) is European or American; every embroidered design is at most 25 cm on its long and short side, and the same design keeps the same real size and the same proportion to the garment in every image; the main chest embroidery of a sweatshirt is centred left to right on the vertical centre line of the garment itself, not of the picture; "
	enP004CuffRules    = "cuff embroidery is only on the cuff of the sleeve on the right side of the final picture; the text baseline is parallel to the cuff seam and the group is centred across the sleeve on the flat part above the seam, not on ribbing or folds; wherever the cuff shows, its design is clearly visible; in the cuff sample and cuff close-up that sleeve hangs straight down with its cuff seam horizontal and the icon and text sit level in the picture without tilting; the sleeve on the left side of the picture has no embroidery; the left-to-right order of the small cuff icon and the name is the same in every image; "
	enP004NoCuffRules  = "neither cuff has any embroidery, text or design; "
	enP004ClarityRules = "the whole picture is sharp, the background too, with no blur, soft focus, depth of field or motion blur."
)

func p004Rules(lang string, cuff bool) string {
	switch {
	case zhPrompt(lang) && cuff:
		return p004FixedRules
	case zhPrompt(lang):
		return p004FixedRulesNoCuff
	case cuff:
		return enP004RulesPrefix + enP004CuffRules + enP004ClarityRules
	}
	return enP004RulesPrefix + enP004NoCuffRules + enP004ClarityRules
}

// enSampleSelfLine and enSampleMasterLine lead a sample's own prompt (see sampleLead).
const enSampleSelfLine = "Detail sample (the other images of this set are drawn from it, so it must be exact): only a close-up of the part that holds this detail (for example part of a cuff), the sleeve straight and the cuff seam horizontal, the icon and text level in the picture without tilting; do not draw anything of the locked spec that is outside this part (chest design, neckline, the whole garment), and do not copy the whole garment from the reference images.\n"

const enSampleMasterLine = "Design master (the other images of this set draw the \"%[1]s\" design from this one, so it must be exact): frame it as the prompt says; the pattern, orientation, position and relative size of the \"%[1]s\" design in this picture are the standard for the whole set and must be clear, complete and straight.\n"

// samplePrefixes start the sample line of a prompt drawn from samples; a
// refinement and a content-check rewording drop that line.
var samplePrefixes = []string{"样板图：参考图", "Samples: reference image"}

func hasSampleLead(prompt string) bool {
	for _, p := range samplePrefixes {
		if strings.HasPrefix(prompt, p) {
			return true
		}
	}
	return false
}

func enSamplePromptLine(n int) string {
	if n <= 1 {
		return "Samples: reference image 1 (the only one) is the detail sample already made for this set and the only source for this design group (pattern, accessories, letter shapes of the name, colours and left-to-right order). Wherever this design group appears in the picture, draw each one exactly like the sample; copy only this detail from the sample, not its framing, background, pose or product colour.\n"
	}
	return fmt.Sprintf("Samples: reference image 1 is the detail sample already made for this set and the only source for this design group (pattern, accessories, letter shapes of the name, colours and left-to-right order). Wherever this design group appears in the picture, draw each one exactly like the sample; ignore the pattern, name and accessories of the same part in the other reference images. Copy only this detail from the sample, not its framing, background, pose or product colour. \"Image 1\" to \"Image %d\" in the prompt below mean reference images %d to %d.\n", n-1, 2, n)
}

func enSamplesPromptLine(areas []string, n int) string {
	parts := []string{}
	for i, area := range areas {
		if area == "" {
			area = "detail"
		}
		parts = append(parts, fmt.Sprintf("image %d is the sample of the \"%s\" design", i+1, area))
	}
	line := "Samples: reference " + strings.Join(parts, ", ") + ". Each sample is the only source for the design of its area (pattern, accessories, orientation, letter shapes and colour of the name, position and relative size on the product): wherever that area appears, draw it exactly like its sample, with no other style, proportion or position; ignore the patterns, names and accessories of these areas in the other reference images. Copy only these designs from the samples, not their framing, background, pose or product colour."
	if rest := n - len(areas); rest > 0 {
		line += fmt.Sprintf(" \"Image 1\" to \"Image %d\" in the prompt below mean reference images %d to %d.", rest, len(areas)+1, n)
	}
	return line + "\n"
}

// The refinement wrapper in English (see refinePrompt).
const (
	enRefinePromptMarker = "\n\nOriginal requirements (only to keep the parts the change does not touch; anything that conflicts with this change no longer applies; do not recompose):\n"
	enRefineHead         = "Edit the first image provided.\nThis change (must be done, and clearly visible afterwards): "
	enRefineHeadRest     = "\nThis change comes before the image as it is and before the original requirements below; where they conflict, follow this change. To make it, the parts directly tied to it may change too, for example to straighten cuff text, the embroidered arm and sleeve may hang straight down with the cuff seam horizontal. Everything else stays exactly the same: product shape and proportions, patterns, text, fonts, colours, people, background and composition."
	enRefineSamplesLine  = "Images 2 to %d are the samples of this set's design areas (for example the chest design or the cuff detail), not the image to edit; use them only to see how these designs should look: draw the patterns, accessories, orientation, text layout, font, colour, position and relative size like the samples, not their garment colour, composition or background."
	enRefineSampleLine   = "Image 2 is this set's detail sample (a close-up such as the cuff), not the image to edit; use it only to see how these details should look: draw the layout of icon and text, the left-to-right order, text direction, font, colour and relative size like the sample, not its garment colour, composition or background."
)

// refineMarkers are the refinement markers of saved prompts, newest first.
var refineMarkers = []string{refinePromptMarker, enRefinePromptMarker, refinePromptMarkerV1}

// enSampleFixMarker starts the corrections added to a redrawn sample's prompt.
const enSampleFixMarker = "\n\nThe previous sample failed its check; correct this time: "

var sampleFixMarkers = []string{sampleFixMarker, enSampleFixMarker}

func sampleFixMarkerIn(lang string) string {
	if zhPrompt(lang) {
		return sampleFixMarker
	}
	return enSampleFixMarker
}

// cutSampleFix drops the corrections of an earlier redraw.
func cutSampleFix(prompt string) string {
	for _, m := range sampleFixMarkers {
		prompt, _, _ = strings.Cut(prompt, m)
	}
	return prompt
}

// enMoodFallback is moodFallback in English: with no AI 模型 to rewrite the
// prompt, the options' English phrases describe the atmosphere.
func enMoodFallback(base string, m moodParts, concept *conceptParts, rendering string) string {
	points := []string{}
	for _, o := range []MoodOption{m.weather, m.time, m.distance, m.expectation} {
		points = append(points, moodIn(o, i18n.En).Phrase)
	}
	if r, ok := findMood(moodRenderings, rendering); ok {
		points = append(points, moodIn(r, i18n.En).Name)
	}
	text := "Atmosphere: " + strings.Join(points, "; ") + "."
	base = strings.TrimSpace(base)
	if base == "" {
		text = "A landscape that fits the atmosphere below, with no product and no text.\n" + text
	}
	if concept != nil {
		text = enConceptFallback(text, concept.form, concept.stance, true)
	}
	if base == "" {
		return text
	}
	return base + "\n" + text
}

// enConceptFallback is conceptFallback in English: the form's and the stance's
// English names and looks.
func enConceptFallback(prompt string, form, stance Concept, single bool) string {
	f, st := conceptIn(form, i18n.En), conceptIn(stance, i18n.En)
	text := fmt.Sprintf("Concept %s × %s: %s; %s.", f.Name, st.Name, f.Look, st.Look)
	prompt = strings.TrimSpace(prompt)
	switch {
	case prompt == "":
		return text
	case single:
		return prompt + "\n" + text
	default:
		return oneLine(prompt) + " " + text
	}
}
