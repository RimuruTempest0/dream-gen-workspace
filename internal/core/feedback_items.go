package core

import (
	"fmt"
	"slices"
	"sort"
	"strings"
)

// 逐张核验 (#20): besides the verdict on the whole 套图 (required), the tester may
// mark single images ok or wrong, with problem types and one sentence. Every
// card can be marked, failed, 结果未知 and paused ones included, and every
// version of a refined image with the refinement asked for.

// feedbackItemVerdicts and feedbackItemProblems are the per-image codes, in the
// order shown.
var (
	feedbackItemVerdicts = []string{"ok", "problem"}
	feedbackItemProblems = []string{"shot-wrong", "too-similar", "design-wrong", "product-changed", "edit-missed", "no-image", "other"}
)

// FeedbackImage is one card (one version of a plan slot) the tester can mark.
type FeedbackImage struct {
	// Slot is the image's number in the suite (1 = first image of the plan).
	Slot       int    `json:"slot"`
	PlanItemID string `json:"planItemID"`
	Purpose    string `json:"purpose"`
	TaskID     string `json:"taskID"`
	// Version counts the slot's attempts from the oldest (1) to Versions.
	Version           int      `json:"version"`
	Versions          int      `json:"versions"`
	Current           bool     `json:"current"`
	Status            string   `json:"status"`
	Error             string   `json:"error,omitempty"`
	AssetIDs          []string `json:"assetIDs"`
	RefineInstruction string   `json:"refineInstruction,omitempty"`
	Sample            bool     `json:"sample,omitempty"`
	SampleArea        string   `json:"sampleArea,omitempty"`
}

// GetFeedbackImages lists the suite's images for per-image verdicts: in the
// plan's slot order, each slot's current version first, then earlier ones.
func (s *Service) GetFeedbackImages(runID string) ([]FeedbackImage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var run AgentRun
	if e := s.store.Get("agent_run", runID, &run); e != nil {
		return nil, missing(e, s.msg("feedback.runMissing"))
	}
	tasks, e := List[Task](s.store, "task")
	if e != nil {
		return nil, e
	}
	return feedbackImagesOf(run, tasks), nil
}

func feedbackImagesOf(run AgentRun, all []Task) []FeedbackImage {
	plan, _ := suitePlanOf(run.Plans, run.SubmittedRevision)
	slots := []string{}
	purpose := map[string]string{}
	for _, item := range plan.Items {
		slots = append(slots, item.ID)
		purpose[item.ID] = item.Purpose
	}
	versions := map[string][]Task{}
	for _, t := range all {
		if t.AgentRunID != run.ID {
			continue
		}
		if _, ok := versions[t.PlanItemID]; !ok && !slices.Contains(slots, t.PlanItemID) {
			slots = append(slots, t.PlanItemID) // a task outside the plan (an older suite)
		}
		versions[t.PlanItemID] = append(versions[t.PlanItemID], t)
	}
	out := []FeedbackImage{}
	for i, slot := range slots {
		list := versions[slot]
		sort.SliceStable(list, func(a, b int) bool { return attemptBefore(list[a], list[b]) })
		for v := len(list) - 1; v >= 0; v-- {
			t := list[v]
			assets := t.AssetIDs
			if assets == nil {
				assets = []string{}
			}
			out = append(out, FeedbackImage{Slot: i + 1, PlanItemID: slot, Purpose: purpose[slot], TaskID: t.ID, Version: v + 1, Versions: len(list), Current: v == len(list)-1, Status: t.Status, Error: t.Error, AssetIDs: assets, RefineInstruction: t.RefineInstruction, Sample: t.Sample, SampleArea: t.SampleArea})
		}
	}
	return out
}

// cleanFeedbackItemsLocked checks the per-image verdicts: each names an image
// of the suite (a task, or a plan slot) and gets its slot from it.
func (s *Service) cleanFeedbackItemsLocked(runID string, items []FeedbackItem) ([]FeedbackItem, error) {
	if len(items) == 0 {
		return nil, nil
	}
	var run AgentRun
	if e := s.store.Get("agent_run", runID, &run); e != nil {
		return nil, missing(e, s.msg("feedback.runMissing"))
	}
	inPlan := map[string]bool{}
	for _, p := range run.Plans {
		for _, item := range p.Items {
			inPlan[item.ID] = true
		}
	}
	out := []FeedbackItem{}
	for _, v := range items {
		if v.TaskID != "" {
			var t Task
			if s.store.Get("task", v.TaskID, &t) != nil || t.AgentRunID != runID || (v.PlanItemID != "" && v.PlanItemID != t.PlanItemID) {
				return nil, s.fail("feedbackItem.imageUnknown")
			}
			v.PlanItemID = t.PlanItemID
		} else if !inPlan[v.PlanItemID] {
			return nil, s.fail("feedbackItem.imageUnknown")
		}
		if !slices.Contains(feedbackItemVerdicts, v.Verdict) {
			return nil, s.fail("feedbackItem.verdictRequired")
		}
		problems := []string{}
		for _, p := range feedbackItemProblems {
			if slices.Contains(v.Problems, p) {
				problems = append(problems, p)
			}
		}
		if len(problems) != len(slices.Compact(slices.Sorted(slices.Values(v.Problems)))) {
			return nil, s.fail("feedbackItem.problemUnknown")
		}
		if v.Verdict == "ok" {
			problems = nil
		}
		v.Problems = problems
		v.Note = strings.TrimSpace(strings.ReplaceAll(v.Note, "\r\n", "\n"))
		if !noteFits(v.Note, maxFeedbackItemNote) {
			return nil, s.fail("feedbackItem.noteTooLong", "max", maxFeedbackItemNote)
		}
		out = append(out, v)
	}
	return out, nil
}

// feedbackItemLabels are the per-image labels of 反馈.md (Chinese, for the provider).
var feedbackItemLabels = map[string]string{
	"ok":              "合格",
	"problem":         "有问题",
	"shot-wrong":      "展示方式不对",
	"too-similar":     "和其他图太像",
	"design-wrong":    "设计区的设计不对",
	"product-changed": "商品本身被改了",
	"edit-missed":     "改图没改到",
	"no-image":        "没出图或报错",
	"other":           "其他",
}

// feedbackItemsMarkdown writes the 逐张结论 section of 反馈.md: each verdict with
// its slot, version, state and the refinement asked for. Empty without verdicts.
// It reads the (redacted) package only.
func feedbackItemsMarkdown(p feedbackPackage) string {
	if len(p.Feedback.Items) == 0 {
		return ""
	}
	run := AgentRun{ID: p.Run.ID, Plans: p.Run.Plans, SubmittedRevision: p.Run.SubmittedRevision}
	tasks := []Task{}
	for _, t := range p.Tasks {
		tasks = append(tasks, Task{ID: t.ID, AgentRunID: run.ID, PlanItemID: t.PlanItemID, CreatedAt: t.CreatedAt, Status: t.Status, RefinedFrom: t.RefinedFrom, RefineInstruction: t.RefineInstruction})
	}
	images := feedbackImagesOf(run, tasks)
	var b strings.Builder
	line := func(format string, a ...any) { fmt.Fprintf(&b, format+"\n", a...) }
	line("## 逐张结论")
	line("")
	for _, v := range p.Feedback.Items {
		var image *FeedbackImage
		for i := range images {
			if images[i].PlanItemID == v.PlanItemID && (v.TaskID == "" || images[i].TaskID == v.TaskID) {
				image = &images[i]
				break
			}
		}
		where := "图位 " + v.PlanItemID
		if image != nil {
			where = fmt.Sprintf("第 %d 张", image.Slot)
			if image.Purpose != "" {
				where += "（" + oneLine(image.Purpose) + "）"
			}
			if v.TaskID != "" {
				current := ""
				if image.Current {
					current = "当前，"
				}
				where += fmt.Sprintf("第 %d 版（%s共 %d 版）", image.Version, current, image.Versions)
			}
		}
		line("- %s：%s", where, feedbackItemLabels[v.Verdict])
		if image != nil && v.TaskID != "" {
			line("  - 图片任务 %s，状态：%s", image.TaskID, feedbackLabel(image.Status))
			if image.RefineInstruction != "" {
				line("  - 修改要求：%s", oneLine(image.RefineInstruction))
			}
		}
		if len(v.Problems) > 0 {
			names := []string{}
			for _, code := range v.Problems {
				names = append(names, feedbackItemLabels[code])
			}
			line("  - 问题类型：%s", strings.Join(names, "；"))
		}
		if v.Note != "" {
			line("  - 说明：%s", oneLine(v.Note))
		}
	}
	line("")
	return b.String()
}
