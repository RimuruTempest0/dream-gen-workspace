package core

import (
	"fmt"
	"strings"
)

// attachSample resolves the samples t waits for (one per design area since 0.7.9).
// It reports false while any sample's current attempt is still queued, generating,
// paused or being checked. Once they are done, the images of the samples that
// succeeded become t's first references and the prompt says which area each
// stands for; a failed sample is left out, so t goes ahead with the others or
// without any (its own prompt never mentions the samples).
func attachSample(t *Task, tasks []Task) bool {
	if !t.SamplePending {
		return true
	}
	items := t.SampleItems
	if len(items) == 0 && t.SampleItem != "" {
		items = []string{t.SampleItem}
	}
	found := []Task{}
	for _, item := range items {
		var sample *Task
		for i := range tasks {
			c := &tasks[i]
			if c.AgentRunID == t.AgentRunID && c.PlanItemID == item && (sample == nil || c.CreatedAt > sample.CreatedAt) {
				sample = c
			}
		}
		if sample == nil {
			continue
		}
		switch sample.Status {
		case "queued", "submitting", "running", "downloading", "blocked":
			return false
		case "succeeded":
			if sample.SampleCheck == "checking" {
				return false
			}
			if len(sample.AssetIDs) > 0 {
				found = append(found, *sample)
			}
		}
	}
	if room := 4 - len(t.References); len(found) > room {
		found = found[:max(room, 0)]
	}
	if len(found) > 0 {
		ids, areas := []string{}, []string{}
		for _, sample := range found {
			ids = append(ids, sample.AssetIDs[0])
			areas = append(areas, sample.SampleArea)
		}
		// First, so the samples outweigh any other picture of the same design.
		t.References = append(append([]string{}, ids...), t.References...)
		t.SampleAssetIDs, t.SampleAssetID = ids, ids[0]
		if len(found) == 1 && areas[0] == "" {
			t.Prompt = samplePromptLine(len(t.References)) + t.Prompt
		} else {
			t.Prompt = samplesPromptLine(areas, len(t.References)) + t.Prompt
		}
	}
	t.SamplePending = false
	t.UpdatedAt = Now()
	return true
}

// samplePromptLine leads a prompt whose first reference is the set's one unnamed
// sample; it keeps the "样板图：参考图第" prefix that refinements and rewordings
// recognise.
func samplePromptLine(n int) string {
	if n <= 1 {
		return "样板图：参考图第 1 张（唯一一张）是本套已经生成好的细节样板，是样板里这组设计（图案、配饰、名字的字形、颜色和左右排列）的唯一依据。画面里每一处出现这组设计，都逐个照样板画，完全相同；只照样板画这部分细节，不照它的构图、背景、姿势或商品颜色。\n"
	}
	return fmt.Sprintf("样板图：参考图第 1 张是本套已经生成好的细节样板，是样板里这组设计（图案、配饰、名字的字形、颜色和左右排列）的唯一依据。画面里每一处出现这组设计，都逐个照样板画，完全相同；其他参考图里同一部位的图案、名字和配饰一律不照。只照样板画这部分细节，不照它的构图、背景、姿势或商品颜色。下面提示词里写的“图1”到“图%d”，依次指第 %d 到第 %d 张参考图。\n", n-1, 2, n)
}

// samplesPromptLine leads a prompt whose first references are the samples of the
// named areas, in order.
func samplesPromptLine(areas []string, n int) string {
	parts := []string{}
	for i, area := range areas {
		if area == "" {
			area = "细节"
		}
		parts = append(parts, fmt.Sprintf("第 %d 张是「%s」设计的样板", i+1, area))
	}
	line := "样板图：参考图" + strings.Join(parts, "，") + "。每张样板是它那个区域设计（图案、配饰、朝向、名字的字形和颜色、在商品上的位置和相对大小）的唯一依据：画面里出现这个区域时照对应样板画，完全相同，不要换成别的画法、比例或位置；其他参考图里这些区域的图案、名字和配饰一律不照。只照样板画这些设计，不照样板的构图、背景、姿势或商品颜色。"
	if rest := n - len(areas); rest > 0 {
		line += fmt.Sprintf("下面提示词里写的“图1”到“图%d”，依次指第 %d 到第 %d 张参考图。", rest, len(areas)+1, n)
	}
	return line + "\n"
}
