package core

import (
	"math"
	"testing"
	"time"
)

func TestSuiteEstimateUsesOnlyMeasuredRecords(t *testing.T) {
	s := testService(t)
	c, e := s.SaveConnection(ConnectionInput{Kind: "image", Name: "图片线路 1", BaseURL: "http://127.0.0.1:1/v1", Model: "m", Key: "k"})
	if e != nil {
		t.Fatal(e)
	}
	// Nothing recorded yet: no numbers at all.
	est, e := s.EstimateSuite(c.ID, "m", "1024x1024", 12)
	if e != nil || est.MedianMS != 0 || est.LLMTokens != 0 || est.Cost != 0 {
		t.Fatalf("estimate without records: %+v %v", est, e)
	}
	var full Connection
	s.store.Get("connection", c.ID, &full)
	full.Managed = "image-1"
	s.store.Put("connection", c.ID, full)

	start := time.Now().Add(-time.Hour)
	for i, ms := range []int64{30000, 40000, 50000, 60000} {
		task := Task{ID: NewID(), Status: "succeeded", Model: "m", Size: "1024x1024", Connection: full, UpdatedAt: start.Add(time.Duration(i+1) * time.Minute).UTC().Format(time.RFC3339Nano), Diagnostic: GenerationDiagnostic{ElapsedMS: ms}}
		s.store.Put("task", task.ID, task)
	}
	s.store.Put("llm_call", "a", LLMCall{ID: "a", Kind: "analysis", RunID: "r1", At: Now(), InputTokens: 1000, OutputTokens: 500})
	s.store.Put("llm_call", "b", LLMCall{ID: "b", Kind: "analysis", RunID: "r1", At: Now(), InputTokens: 2000, OutputTokens: 500})
	s.store.Put("llm_call", "c", LLMCall{ID: "c", Kind: "analysis", RunID: "r2", At: Now(), InputTokens: 1000, OutputTokens: 0})
	s.store.Put("llm_call", "d", LLMCall{ID: "d", Kind: "test", At: Now(), InputTokens: 99999})

	s.mu.Lock()
	s.recordUsageLocked(upstreamUsage{at: start, remaining: 1.0, unit: "USD", known: true}, upstreamUsage{at: time.Now(), remaining: 0.6, unit: "USD", known: true})
	s.mu.Unlock()
	est, e = s.EstimateSuite(c.ID, "m", "1024x1024", 12)
	if e != nil {
		t.Fatal(e)
	}
	if est.MedianMS != 50000 || est.DurationSamples != 4 {
		t.Fatalf("duration %+v", est)
	}
	if est.LLMTokens != 2500 || est.LLMRuns != 2 {
		t.Fatalf("tokens %+v", est)
	}
	if math.Abs(est.CostPerImage-0.1) > 1e-9 || math.Abs(est.Cost-1.2) > 1e-9 || est.CostUnit != "USD" {
		t.Fatalf("cost %+v", est)
	}
	// Another size has no measured cost or duration.
	if other, _ := s.EstimateSuite(c.ID, "m", "1536x1024", 12); other.Cost != 0 || other.MedianMS != 0 {
		t.Fatalf("other size %+v", other)
	}
}

func TestMixedImagesDoNotMeasureCost(t *testing.T) {
	s := testService(t)
	c, _ := s.SaveConnection(ConnectionInput{Kind: "image", Name: "图片线路 1", BaseURL: "http://127.0.0.1:1/v1", Model: "m", Key: "k"})
	var full Connection
	s.store.Get("connection", c.ID, &full)
	full.Managed = "image-1"
	s.store.Put("connection", c.ID, full)
	start := time.Now().Add(-time.Hour)
	for i, size := range []string{"1024x1024", "1024x1024", "1536x1024", "1024x1024"} {
		task := Task{ID: NewID(), Status: "succeeded", Model: "m", Size: size, Connection: full, UpdatedAt: start.Add(time.Duration(i+1) * time.Minute).UTC().Format(time.RFC3339Nano)}
		s.store.Put("task", task.ID, task)
	}
	s.mu.Lock()
	s.recordUsageLocked(upstreamUsage{at: start, remaining: 1.0, unit: "USD", known: true}, upstreamUsage{at: time.Now(), remaining: 0.6, unit: "USD", known: true})
	s.mu.Unlock()
	if est, _ := s.EstimateSuite(c.ID, "m", "1024x1024", 12); est.Cost != 0 {
		t.Fatalf("mixed window priced: %+v", est)
	}
}
