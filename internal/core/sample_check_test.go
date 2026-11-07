package core

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func widePNG(w, h int) []byte {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := color.NRGBA{200, 40, 40, 255}
			if x >= w/2 {
				c = color.NRGBA{40, 40, 200, 255}
			}
			img.Set(x, y, c)
		}
	}
	var b bytes.Buffer
	png.Encode(&b, img)
	return b.Bytes()
}

// The Agent cuts one style out of a comparison picture and may plan with the cut.
func TestCropReferenceSavesACleanReference(t *testing.T) {
	s := testService(t)
	source, e := s.ImportImage("comparison.png", base64.StdEncoding.EncodeToString(widePNG(400, 200)))
	if e != nil {
		t.Fatal(e)
	}
	other, _ := s.ImportImage("unrelated.png", base64.StdEncoding.EncodeToString(widePNG(100, 100)))
	r := AgentRun{ID: NewID(), CreatedAt: Now(), Status: "analyzing", Product: ProductInfo{ReferenceIDs: []string{source.ID}}}
	r.LLMConnection.Capabilities.Vision = true
	s.store.Put("agent_run", r.ID, r)
	b := &agentBridge{service: s, runID: r.ID, ctx: context.Background()}
	call := func(v map[string]any) ([]any, error) {
		raw, _ := json.Marshal(v)
		return b.callTool("crop_reference", raw)
	}
	for name, bad := range map[string]map[string]any{
		"not this run's": {"assetID": other.ID, "left": 0, "top": 0, "width": 1, "height": 1, "label": "x"},
		"outside":        {"assetID": source.ID, "left": 0.6, "top": 0, "width": 0.6, "height": 1, "label": "x"},
		"too small":      {"assetID": source.ID, "left": 0, "top": 0, "width": 0.05, "height": 0.05, "label": "x"},
	} {
		if _, e := call(bad); e == nil {
			t.Fatal("crop accepted:", name)
		}
	}
	out, e := call(map[string]any{"assetID": source.ID, "left": 0.5, "top": 0, "width": 0.5, "height": 1, "label": "右边那一款"})
	if e != nil {
		t.Fatal(e)
	}
	if len(out) != 2 || out[1].(map[string]any)["type"] != "image" {
		t.Fatal("the cut is not shown to the Agent", out)
	}
	saved, _ := s.GetAgentRun(r.ID)
	if len(saved.Crops) != 1 {
		t.Fatal("crop not recorded", saved.Crops)
	}
	var a Asset
	s.store.Get("asset", saved.Crops[0], &a)
	data, _ := readOwned(s.store.Root, a.Path)
	img, _, e := image.Decode(bytes.NewReader(data))
	if e != nil || img.Bounds().Dx() != cropTargetSide || img.Bounds().Dy() != cropTargetSide {
		t.Fatalf("cut not enlarged to a usable size: %v %v", img.Bounds(), e)
	}
	if r, _, bl, _ := img.At(10, 10).RGBA(); bl <= r {
		t.Fatal("cut the wrong half")
	}
	// The cut can be used in the plan; another run's image cannot.
	ok := SuitePlan{Items: []SuiteItem{{Prompt: "p", References: []string{saved.Crops[0]}}}}
	if e := s.validatePlan(&saved, &ok); e != nil {
		t.Fatal(e)
	}
	bad := SuitePlan{Items: []SuiteItem{{Prompt: "p", References: []string{other.ID}}}}
	if e := s.validatePlan(&saved, &bad); e == nil {
		t.Fatal("unrelated image allowed in the plan")
	}
}

// checkedSampleFixture is sampleFixture with an LLM line whose checks of the
// sample answer verdicts in order.
func checkedSampleFixture(t *testing.T, verdicts ...string) (*Service, string, map[string][]*sampleCall, *sync.Mutex, *atomic.Int32) {
	t.Helper()
	var mu sync.Mutex
	calls := map[string][]*sampleCall{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := &sampleCall{start: time.Now()}
		if r.URL.Path == "/images/edits" {
			r.ParseMultipartForm(8 << 20)
			call.prompt = r.FormValue("prompt")
			call.images = len(r.MultipartForm.File["image"]) + len(r.MultipartForm.File["image[]"])
		} else {
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			call.prompt, _ = body["prompt"].(string)
		}
		slot := strings.Fields(call.prompt[strings.LastIndex(call.prompt, "SLOT:"):])[0]
		call.done = time.Now()
		mu.Lock()
		calls[slot] = append(calls[slot], call)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"data": []map[string]string{{"b64_json": base64.StdEncoding.EncodeToString(tinyPNG())}}})
	}))
	t.Cleanup(server.Close)
	var checks atomic.Int32
	llm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []struct {
				Content []map[string]any `json:"content"`
			} `json:"messages"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		images := 0
		for _, block := range body.Messages[0].Content {
			if block["type"] == "image" {
				images++
			}
		}
		n := int(checks.Add(1))
		verdict := verdicts[len(verdicts)-1]
		if n <= len(verdicts) {
			verdict = verdicts[n-1]
		}
		if images < 2 {
			verdict = `{"pass": false, "problems": ["没收到样板和设计依据"]}`
		}
		json.NewEncoder(w).Encode(map[string]any{"content": []map[string]string{{"type": "text", "text": verdict}}})
	}))
	t.Cleanup(llm.Close)
	s := testService(t)
	if _, e := s.SaveConnection(ConnectionInput{Kind: "llm", Name: "LLM", BaseURL: llm.URL, Model: "m", Key: "k"}); e != nil {
		t.Fatal(e)
	}
	saved := saveConnection(t, s, server.URL)
	var c Connection
	s.store.Get("connection", saved.ID, &c)
	design, _ := s.ImportImage("design.png", base64.StdEncoding.EncodeToString(tinyPNG()))
	run := AgentRun{ID: NewID(), CreatedAt: Now(), Status: "submitted", BatchID: "batch-c"}
	s.store.Put("agent_run", run.ID, run)
	for _, task := range []Task{
		{PlanItemID: "sample", Sample: true, References: []string{design.ID}},
		{PlanItemID: "uses", SampleItem: "sample", SamplePending: true, References: []string{design.ID}},
	} {
		task.ID, task.BatchID, task.AgentRunID, task.CreatedAt, task.UpdatedAt = NewID(), "batch-c", run.ID, Now(), Now()
		task.Status, task.Prompt, task.Model, task.Size, task.Connection = "queued", "头饰是迷你圣诞袜 SLOT:"+task.PlanItemID, "test-image", "1024x1024", c
		task.AssetIDs = []string{}
		s.store.Put("task", task.ID, task)
	}
	s.Start(func() {})
	return s, run.ID, calls, &mu, &checks
}

// A wrong sample is redrawn with the problems named; images drawn from it wait
// for the sample that passed and get it as their first reference.
func TestWrongSampleIsRedrawnBeforeTheSetCopiesIt(t *testing.T) {
	s, runID, calls, mu, checks := checkedSampleFixture(t, `{"pass": false, "problems": ["头饰画成了圣诞帽，应是迷你圣诞袜"]}`, `{"pass": true, "problems": []}`)
	uses := waitTask(t, s, runID, "uses", func(task Task) bool { return task.Status == "succeeded" })
	all, _ := List[Task](s.store, "task")
	samples := []Task{}
	for _, x := range all {
		if x.PlanItemID == "sample" {
			samples = append(samples, x)
		}
	}
	if len(samples) != 2 || checks.Load() != 2 {
		t.Fatalf("expected one redraw: %d samples, %d checks", len(samples), checks.Load())
	}
	newest, first := samples[0], samples[1] // newest first
	if first.SampleCheck != "redrawn" || newest.SampleCheck != "passed" || newest.SampleRedraws != 1 || !strings.Contains(newest.Prompt, sampleFixMarker+"头饰画成了圣诞帽") {
		t.Fatalf("redraw not recorded: %+v / %+v", first, newest)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(calls["SLOT:sample"]) != 2 || calls["SLOT:uses"][0].start.Before(calls["SLOT:sample"][1].done) {
		t.Fatal("the set did not wait for the redrawn sample")
	}
	if uses.SampleAssetID != newest.AssetIDs[0] || uses.References[0] != newest.AssetIDs[0] || len(uses.References) != 2 {
		t.Fatalf("passed sample is not the first reference: %+v", uses.References)
	}
	if p := calls["SLOT:uses"][0].prompt; !strings.HasPrefix(p, samplePromptLine(2)) || !strings.Contains(p, "唯一依据") {
		t.Fatalf("sample line missing: %q", p)
	}
	llmCalls, _ := List[LLMCall](s.store, "llm_call")
	if len(llmCalls) != 2 || llmCalls[0].Kind != "check" {
		t.Fatalf("checks not recorded: %+v", llmCalls)
	}
}

// After the redraws the set goes ahead with the last sample and says so.
func TestSampleStillWrongAfterRedrawsLetsTheSetGoOn(t *testing.T) {
	s, runID, _, _, checks := checkedSampleFixture(t, `{"pass": false, "problems": ["名字拼错"]}`)
	waitTask(t, s, runID, "uses", func(task Task) bool { return task.Status == "succeeded" })
	all, _ := List[Task](s.store, "task")
	samples := 0
	var last Task
	for _, x := range all {
		if x.PlanItemID == "sample" {
			if samples == 0 {
				last = x
			}
			samples++
		}
	}
	if samples != 1+maxSampleRedraws || checks.Load() != int32(1+maxSampleRedraws) || last.SampleCheck != "failed" || !strings.Contains(last.SampleProblems, "名字拼错") {
		t.Fatalf("%d samples, %d checks, last %+v", samples, checks.Load(), last)
	}
	run, _ := s.GetAgentRun(runID)
	found := false
	for _, a := range run.Activity {
		found = found || strings.Contains(a.Text, "仍没通过核对")
	}
	if !found {
		t.Fatal("the final failed check is not reported")
	}
}
