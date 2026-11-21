package core

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// photoPNG is a w×h picture with gradients and grain, so it compresses roughly
// like a photo rather than a flat test colour.
func photoPNG(w, h int, seed int64) []byte {
	r := rand.New(rand.NewSource(seed))
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			n := uint8(r.Intn(24))
			img.Set(x, y, color.NRGBA{uint8(x*200/w) + n, uint8(y*180/h) + n, uint8((x+y)*90/(w+h)) + 60, 255})
		}
	}
	var b bytes.Buffer
	png.Encode(&b, img)
	return b.Bytes()
}

func putAsset(t testing.TB, s *Service, data []byte, kind, taskID string) Asset {
	t.Helper()
	a, e := s.saveImage(data, "图.png", kind, taskID, "", "")
	if e != nil {
		t.Fatal(e)
	}
	return a
}

type packagedImage struct {
	File       string `json:"file"`
	AssetID    string `json:"assetID"`
	Kind       string `json:"kind"`
	Source     string `json:"source"`
	TaskID     string `json:"taskID"`
	PlanItemID string `json:"planItemID"`
	Slot       int    `json:"slot"`
	Version    int    `json:"version"`
	Sample     bool   `json:"sample"`
	Width      int    `json:"width"`
	Height     int    `json:"height"`
}

// A suite with an uploaded product photo, a product-page image, a crop, a
// design-area sample, a refined image (two versions) and a failed image.
type imageSuite struct {
	run                          AgentRun
	upload, page, crop           Asset
	sample, first, second, other Asset
}

func putImageSuite(t *testing.T, s *Service) imageSuite {
	t.Helper()
	var v imageSuite
	v.upload = putAsset(t, s, photoPNG(2400, 1800, 1), "reference", "")
	v.page = putAsset(t, s, photoPNG(800, 800, 2), "reference", "")
	v.crop = putAsset(t, s, photoPNG(1024, 600, 3), "reference", "")
	plan := SuitePlan{Revision: 1, CreatedAt: Now(), Items: []SuiteItem{
		{ID: "s1", Purpose: "胸前样板", Sample: true, SampleArea: "胸前", References: []string{v.crop.ID}},
		{ID: "p1", Purpose: "上身主图", References: []string{v.upload.ID}},
		{ID: "p2", Purpose: "整件平铺", References: []string{v.upload.ID}},
	}}
	v.run = AgentRun{ID: NewID(), CreatedAt: Now(), UpdatedAt: Now(), Status: "submitted", SubmittedRevision: 1, Plans: []SuitePlan{plan},
		Input: AgentInput{Instruction: "套图", References: []string{v.upload.ID}}, Product: ProductInfo{ReferenceIDs: []string{v.page.ID}}, Crops: []string{v.crop.ID}}
	s.store.Put("agent_run", v.run.ID, v.run)
	at := time.Now()
	task := func(item string, mod func(*Task), images ...Asset) Task {
		at = at.Add(time.Second)
		tk := Task{ID: NewID(), BatchID: "b", AgentRunID: v.run.ID, PlanItemID: item, CreatedAt: at.UTC().Format(time.RFC3339Nano), Status: "succeeded", Prompt: item, AssetIDs: []string{}}
		for _, a := range images {
			tk.AssetIDs = append(tk.AssetIDs, a.ID)
		}
		if mod != nil {
			mod(&tk)
		}
		s.store.Put("task", tk.ID, tk)
		return tk
	}
	v.sample = putAsset(t, s, photoPNG(1024, 1024, 4), "generated", "")
	task("s1", func(k *Task) { k.Sample, k.SampleArea, k.References = true, "胸前", []string{v.crop.ID} }, v.sample)
	v.first = putAsset(t, s, photoPNG(2000, 3000, 5), "generated", "")
	one := task("p1", func(k *Task) {
		k.References, k.SampleAssetIDs = []string{v.sample.ID, v.upload.ID}, []string{v.sample.ID}
	}, v.first)
	v.second = putAsset(t, s, photoPNG(1024, 1536, 6), "generated", "")
	task("p1", func(k *Task) {
		k.RefinedFrom, k.RefineInstruction, k.References = one.ID, "亮一点", []string{v.first.ID}
	}, v.second)
	task("p2", func(k *Task) { k.Status, k.Error = "failed", "上游错误" })
	v.other = putAsset(t, s, photoPNG(600, 900, 7), "generated", "")
	task("p2", nil, v.other)
	return v
}

func packagedImages(t *testing.T, files map[string][]byte) []packagedImage {
	t.Helper()
	var pkg struct {
		Images []packagedImage `json:"images"`
	}
	if e := json.Unmarshal(files["run.json"], &pkg); e != nil {
		t.Fatal(e)
	}
	return pkg.Images
}

func TestFeedbackPackageHoldsEveryImageAsJPEGNamedAsInRunJSON(t *testing.T) {
	s := testService(t)
	v := putImageSuite(t, s)
	names, files := feedbackZip(t, s, v.run.ID)
	images := packagedImages(t, files)
	listed := map[string]packagedImage{}
	for _, img := range images {
		listed[img.File] = img
	}
	inZip := []string{}
	for _, n := range names {
		if strings.HasPrefix(n, "images/") {
			inZip = append(inZip, n)
		}
	}
	// 4 generated (sample, both versions, the other slot) and 3 references.
	if len(images) != 7 || len(inZip) != 7 || len(listed) != 7 {
		t.Fatalf("run.json lists %v, the package holds %v", images, inZip)
	}
	md := string(files["反馈.md"])
	for _, n := range inZip {
		img, ok := listed[n]
		if !ok {
			t.Fatalf("%s is not in run.json", n)
		}
		config, format, e := image.DecodeConfig(bytes.NewReader(files[n]))
		if e != nil || format != "jpeg" || max(config.Width, config.Height) > 1600 || config.Width != img.Width || config.Height != img.Height {
			t.Fatalf("%s: %s %dx%d %v (run.json %dx%d)", n, format, config.Width, config.Height, e, img.Width, img.Height)
		}
		if !strings.Contains(md, n) {
			t.Fatalf("反馈.md does not name %s", n)
		}
	}
	// Large images are brought down to a long side of about 1600 pixels.
	byAsset := map[string]packagedImage{}
	for _, img := range images {
		byAsset[img.AssetID] = img
	}
	if u := byAsset[v.upload.ID]; u.Width != 1600 || u.Height != 1200 {
		t.Fatalf("upload packed at %dx%d", u.Width, u.Height)
	}
	if f := byAsset[v.first.ID]; f.Height != 1600 {
		t.Fatalf("first version packed at %dx%d", f.Width, f.Height)
	}
	if o := byAsset[v.other.ID]; o.Width != 600 || o.Height != 900 {
		t.Fatalf("a small image was resized: %dx%d", o.Width, o.Height)
	}
}

func TestFeedbackPackageHoldsSamplesVersionsAndCrops(t *testing.T) {
	s := testService(t)
	v := putImageSuite(t, s)
	_, files := feedbackZip(t, s, v.run.ID)
	byAsset := map[string]packagedImage{}
	for _, img := range packagedImages(t, files) {
		byAsset[img.AssetID] = img
	}
	want := map[string]packagedImage{
		v.sample.ID: {Kind: "generated", PlanItemID: "s1", Slot: 1, Version: 1, Sample: true},
		v.first.ID:  {Kind: "generated", PlanItemID: "p1", Slot: 2, Version: 1},
		v.second.ID: {Kind: "generated", PlanItemID: "p1", Slot: 2, Version: 2},
		v.other.ID:  {Kind: "generated", PlanItemID: "p2", Slot: 3, Version: 1},
		v.upload.ID: {Kind: "reference", Source: "upload"},
		v.page.ID:   {Kind: "reference", Source: "product"},
		v.crop.ID:   {Kind: "reference", Source: "crop"},
	}
	for id, w := range want {
		got, ok := byAsset[id]
		if !ok || got.File == "" || got.Kind != w.Kind || got.Source != w.Source || got.PlanItemID != w.PlanItemID || got.Slot != w.Slot || got.Version != w.Version || got.Sample != w.Sample {
			t.Errorf("%s: got %+v, want %+v", id, got, w)
		}
		if w.Kind == "generated" && got.TaskID == "" {
			t.Errorf("%s: no task", id)
		}
		if _, ok := files[got.File]; !ok {
			t.Errorf("%s: %s missing from the package", id, got.File)
		}
	}
	md := string(files["反馈.md"])
	for _, text := range []string{"样板", "第 2 版", "亮一点", "顾客上传的商品原图", "截出的局部"} {
		if !strings.Contains(md, text) {
			t.Errorf("反馈.md does not say %q", text)
		}
	}
}

func TestFeedbackPackageReportsProgressPerImageAndLeavesTheQueueRunning(t *testing.T) {
	relay, server := newSlowRelay(t)
	close(relay.release)
	s := testService(t)
	v := putImageSuite(t, s)
	c := saveConnection(t, s, server.URL)
	s.Start(func() {})
	var mu sync.Mutex
	steps := []FeedbackPackageProgress{}
	generated := false
	dest := filepath.Join(t.TempDir(), "问题反馈.zip")
	e := s.ExportFeedbackPackageWithProgress(v.run.ID, dest, func(p FeedbackPackageProgress) {
		mu.Lock()
		steps = append(steps, p)
		first := len(steps) == 1
		mu.Unlock()
		if !first {
			return
		}
		// While the package is being made, an image is generated start to finish.
		done := make(chan bool, 1)
		go func() {
			batch, e := s.Submit(request(c))
			if e != nil {
				done <- false
				return
			}
			for i := 0; i < 400; i++ {
				tasks, _ := List[Task](s.store, "task")
				for _, k := range tasks {
					if k.BatchID == batch && k.Status == "succeeded" {
						done <- true
						return
					}
				}
				time.Sleep(20 * time.Millisecond)
			}
			done <- false
		}()
		select {
		case generated = <-done:
		case <-time.After(10 * time.Second):
		}
	})
	if e != nil {
		t.Fatal(e)
	}
	if !generated {
		t.Fatal("the image queue stopped while the package was made")
	}
	if len(steps) != 8 {
		t.Fatalf("progress %+v", steps)
	}
	for i, p := range steps[:7] {
		if p.RunID != v.run.ID || p.Image != i+1 || p.Images != 7 || p.Done {
			t.Fatalf("step %d: %+v", i, p)
		}
	}
	if last := steps[7]; !last.Done || last.Image != 7 || last.Images != 7 {
		t.Fatalf("last step %+v", last)
	}
	if _, e = os.Stat(dest); e != nil {
		t.Fatal(e)
	}
}

func TestFeedbackPackagePreviewCountsImagesAndUploads(t *testing.T) {
	s := testService(t)
	v := putImageSuite(t, s)
	preview, e := s.PreviewFeedbackPackage(v.run.ID)
	if e != nil {
		t.Fatal(e)
	}
	if preview.GeneratedImages != 4 || preview.ReferenceImages != 3 || preview.Uploads != 1 {
		t.Fatalf("preview %+v", preview)
	}
	names, files := feedbackZip(t, s, v.run.ID)
	if len(preview.Files) != len(names) {
		t.Fatalf("preview lists %d files, the package holds %d", len(preview.Files), len(names))
	}
	var total int64
	for i, f := range preview.Files {
		if f.Name != names[i] || f.Bytes <= 0 {
			t.Fatalf("preview %+v, package %v", preview.Files, names)
		}
		total += int64(len(files[f.Name]))
	}
	// Images are estimated, not encoded, for the preview: the right order of size.
	if preview.Bytes < total/3 || preview.Bytes > total*3 {
		t.Fatalf("preview estimates %d bytes, the package holds %d", preview.Bytes, total)
	}
	// A suite without an upload says so.
	bare := putFeedbackRun(t, s, "ready")
	if p, _ := s.PreviewFeedbackPackage(bare.ID); p.Uploads != 0 || p.GeneratedImages != 0 || p.ReferenceImages != 0 {
		t.Fatalf("bare preview %+v", p)
	}
}

// Measures how long a suite of about 20 images takes to pack; logged, not asserted.
func TestFeedbackPackageTimeForTwentyImages(t *testing.T) {
	if testing.Short() {
		t.Skip("timing")
	}
	s := testService(t)
	upload := putAsset(t, s, photoPNG(2000, 2000, 100), "reference", "")
	run := AgentRun{ID: NewID(), CreatedAt: Now(), UpdatedAt: Now(), Status: "submitted", Input: AgentInput{Instruction: "20 张", References: []string{upload.ID}}}
	s.store.Put("agent_run", run.ID, run)
	for i := 0; i < 20; i++ {
		a := putAsset(t, s, photoPNG(1024, 1536, int64(i)), "generated", "")
		tk := Task{ID: NewID(), BatchID: "b", AgentRunID: run.ID, PlanItemID: "p" + string(rune('a'+i)), CreatedAt: Now(), Status: "succeeded", AssetIDs: []string{a.ID}, References: []string{upload.ID}}
		s.store.Put("task", tk.ID, tk)
	}
	preview, _ := s.PreviewFeedbackPackage(run.ID)
	dest := filepath.Join(t.TempDir(), "问题反馈.zip")
	start := time.Now()
	if e := s.ExportFeedbackPackage(run.ID, dest); e != nil {
		t.Fatal(e)
	}
	took := time.Since(start)
	info, _ := os.Stat(dest)
	t.Logf("feedback package, 20 generated 1024x1536 + 1 reference 2000x2000: %v, %.1f MB (preview estimated %.1f MB)", took.Round(time.Millisecond), float64(info.Size())/1048576, float64(preview.Bytes)/1048576)
}
