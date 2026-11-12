package core

import (
	"sort"
	"time"
)

// What a smart suite is likely to take, from this data folder's own records only:
// the median time of recent images of the same model and size, the average LLM
// tokens of recent analyses, and the cost per image measured from the main line's
// upstream quota (customer packages that can read it). Anything without records
// is left empty; no price is ever assumed.

type SuiteEstimate struct {
	Images          int     `json:"images"`
	MedianMS        int64   `json:"medianMS,omitempty"`
	DurationSamples int     `json:"durationSamples,omitempty"`
	LLMTokens       int64   `json:"llmTokens,omitempty"`
	LLMRuns         int     `json:"llmRuns,omitempty"`
	CostPerImage    float64 `json:"costPerImage,omitempty"`
	Cost            float64 `json:"cost,omitempty"`
	CostUnit        string  `json:"costUnit,omitempty"`
	CostSamples     int     `json:"costSamples,omitempty"`
}

// imageCost accumulates measured upstream spending per model and size.
type imageCost struct {
	Spent  float64 `json:"spent"`
	Images float64 `json:"images"`
	Unit   string  `json:"unit"`
}

const (
	imageCostPrefKey    = "image-cost"
	imageCostWindow     = 50 // images kept in the running average
	minCostSamples      = 3
	estimateRecentTasks = 20
	estimateRecentRuns  = 10
)

func costKey(model, size string) string { return model + "|" + size }

// recordUsageLocked compares a new upstream reading with the previous one: when
// the quota dropped and every image finished on the main line in between had the
// same model and size, the drop is shared among them.
func (s *Service) recordUsageLocked(prev, next upstreamUsage) {
	if !prev.known || !next.known || prev.unit != next.unit || prev.at.IsZero() {
		return
	}
	spent := prev.remaining - next.remaining
	if spent <= 0 {
		return
	}
	tasks, e := List[Task](s.store, "task")
	if e != nil {
		return
	}
	main := s.managedLineIDLocked("image-1")
	key, n := "", 0
	for _, t := range tasks {
		if main == "" || t.Connection.ID != main || (t.Status != "succeeded" && t.Status != "unknown" && t.Status != "download_failed") {
			continue
		}
		at, e := time.Parse(time.RFC3339Nano, t.UpdatedAt)
		if e != nil || !at.After(prev.at) || at.After(next.at) {
			continue
		}
		k := costKey(t.Model, taskSize(t))
		if key != "" && k != key {
			return // mixed models or sizes: the drop cannot be split fairly
		}
		key, n = k, n+1
	}
	if n == 0 {
		return
	}
	costs := map[string]imageCost{}
	_ = s.store.Get("preferences", imageCostPrefKey, &costs)
	c := costs[key]
	if c.Unit != "" && c.Unit != next.unit {
		c = imageCost{}
	}
	c.Spent, c.Images, c.Unit = c.Spent+spent, c.Images+float64(n), next.unit
	if c.Images > imageCostWindow {
		c.Spent *= imageCostWindow / c.Images
		c.Images = imageCostWindow
	}
	costs[key] = c
	_ = s.store.Put("preferences", imageCostPrefKey, costs)
}

// EstimateSuite estimates count images of model and size on connectionID.
func (s *Service) EstimateSuite(connectionID, model, size string, count int) (SuiteEstimate, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	est := SuiteEstimate{Images: count}
	var c Connection
	if connectionID != "" && s.store.Get("connection", connectionID, &c) == nil && model == "" {
		model = c.Model
	}
	if size == "" {
		size = "auto"
	}
	tasks, e := List[Task](s.store, "task")
	if e != nil {
		return est, e
	}
	done := []Task{}
	for _, t := range tasks {
		if t.Status == "succeeded" && t.Model == model && taskSize(t) == size && t.Diagnostic.ElapsedMS > 0 {
			done = append(done, t)
		}
	}
	sort.Slice(done, func(i, j int) bool { return done[i].UpdatedAt > done[j].UpdatedAt })
	if len(done) > estimateRecentTasks {
		done = done[:estimateRecentTasks]
	}
	if len(done) > 0 {
		ms := []int64{}
		for _, t := range done {
			ms = append(ms, t.Diagnostic.ElapsedMS)
		}
		sort.Slice(ms, func(i, j int) bool { return ms[i] < ms[j] })
		est.MedianMS, est.DurationSamples = ms[len(ms)/2], len(ms)
	}
	calls, e := List[LLMCall](s.store, "llm_call")
	if e != nil {
		return est, e
	}
	perRun := map[string]int64{}
	last := map[string]string{}
	for _, call := range calls {
		if call.Kind != "analysis" || call.RunID == "" {
			continue
		}
		perRun[call.RunID] += call.InputTokens + call.OutputTokens
		if call.At > last[call.RunID] {
			last[call.RunID] = call.At
		}
	}
	runs := []string{}
	for id, n := range perRun {
		if n > 0 {
			runs = append(runs, id)
		}
	}
	sort.Slice(runs, func(i, j int) bool { return last[runs[i]] > last[runs[j]] })
	if len(runs) > estimateRecentRuns {
		runs = runs[:estimateRecentRuns]
	}
	if len(runs) > 0 {
		var total int64
		for _, id := range runs {
			total += perRun[id]
		}
		est.LLMTokens, est.LLMRuns = total/int64(len(runs)), len(runs)
	}
	if c.Managed == "image-1" {
		costs := map[string]imageCost{}
		_ = s.store.Get("preferences", imageCostPrefKey, &costs)
		if v := costs[costKey(model, size)]; v.Images >= minCostSamples && v.Spent > 0 {
			est.CostPerImage = v.Spent / v.Images
			est.Cost = est.CostPerImage * float64(count)
			est.CostUnit, est.CostSamples = v.Unit, int(v.Images)
		}
	}
	return est, nil
}

func (s *Service) managedLineIDLocked(slot string) string {
	cs, _ := List[Connection](s.store, "connection")
	for _, c := range cs {
		if c.Managed == slot {
			return c.ID
		}
	}
	return ""
}

func taskSize(t Task) string {
	if t.Size == "" {
		return "auto"
	}
	return t.Size
}
