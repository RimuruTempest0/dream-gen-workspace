//go:build desktopqa

package core

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"time"
)

// The desktop QA build packs feedback images slowly, so the end-to-end test
// sees the "packing x/y" progress.
func init() { feedbackPackDelay = 400 * time.Millisecond }

func qaPNG(w, h int, c color.NRGBA) []byte {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, c)
		}
	}
	var b bytes.Buffer
	png.Encode(&b, img)
	return b.Bytes()
}

// SeedTestImageSuite saves a finished suite with images, without generating
// anything: one uploaded product photo and one crop as references, a sample and
// two versions of one image. It returns the run ID.
func (s *Service) SeedTestImageSuite() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	upload, e := s.saveImage(qaPNG(2400, 1800, color.NRGBA{200, 60, 60, 255}), "qa-upload.png", "reference", "", "", "")
	if e != nil {
		return "", e
	}
	crop, e := s.saveImage(qaPNG(600, 400, color.NRGBA{60, 160, 60, 255}), "qa-crop.png", "reference", "", "", "")
	if e != nil {
		return "", e
	}
	run := AgentRun{ID: NewID(), CreatedAt: Now(), UpdatedAt: Now(), Status: "submitted", Stage: "QA", BatchID: NewID(), SubmittedRevision: 1, Language: s.Language(),
		Input:     AgentInput{Instruction: "QA suite: worn front with images", TargetCount: 2, Size: "1024x1024", Quality: "auto", References: []string{upload.ID}, SkillIDs: []string{}},
		Questions: []AgentQuestion{}, Events: []AgentEvent{{At: Now(), Text: "QA plan saved"}}, Product: ProductInfo{ReferenceIDs: []string{}}, Crops: []string{crop.ID},
		Plans: []SuitePlan{{Revision: 1, CreatedAt: Now(), Locked: []string{"QA product"}, Editable: []string{}, Items: []SuiteItem{
			{ID: "qa-s", Purpose: "chest sample", Sample: true, SampleArea: "chest", Prompt: "QA sample", References: []string{crop.ID}},
			{ID: "qa-1", Purpose: "worn front", Scene: "studio", Prompt: "QA prompt 1", References: []string{upload.ID}},
		}}}}
	if e = s.store.Put("agent_run", run.ID, run); e != nil {
		return "", e
	}
	at := time.Now()
	var previous string
	for i, item := range []string{"qa-s", "qa-1", "qa-1"} {
		at = at.Add(time.Second)
		id := NewID()
		a, e := s.saveImage(qaPNG(1024, 1024, color.NRGBA{uint8(60 * i), 90, 200, 255}), "", "generated", id, "", "")
		if e != nil {
			return "", e
		}
		t := Task{ID: id, BatchID: run.BatchID, AgentRunID: run.ID, PlanItemID: item, CreatedAt: at.UTC().Format(time.RFC3339Nano), UpdatedAt: Now(), Status: "succeeded", Prompt: item, Model: "qa-image", Size: "1024x1024", Quality: "auto", References: []string{}, AssetIDs: []string{a.ID}, Sample: item == "qa-s", SampleArea: map[bool]string{true: "chest"}[item == "qa-s"]}
		if i == 2 {
			t.RefinedFrom, t.RefineInstruction = previous, "brighter"
		}
		previous = t.ID
		if e = s.store.Put("task", t.ID, t); e != nil {
			return "", e
		}
	}
	return run.ID, nil
}
