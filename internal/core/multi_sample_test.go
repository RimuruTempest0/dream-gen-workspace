package core

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func areaPlan(items ...SuiteItem) SuitePlan {
	for i := range items {
		if items[i].Prompt == "" {
			items[i].Prompt = "p"
		}
		if items[i].References == nil {
			items[i].References = []string{}
		}
	}
	return SuitePlan{Items: items}
}

func TestPlanAllowsOneSamplePerDesignArea(t *testing.T) {
	s := testService(t)
	r := AgentRun{Input: AgentInput{References: []string{"a", "b", "c", "d"}}}
	chest := SuiteItem{Sample: true, SampleArea: "胸前"}
	cuff := SuiteItem{Sample: true, SampleArea: "袖口"}
	for name, p := range map[string]SuitePlan{
		"same area twice":     areaPlan(chest, SuiteItem{Sample: true, SampleArea: "胸前"}),
		"unnamed with two":    areaPlan(chest, SuiteItem{Sample: true}),
		"four samples":        areaPlan(chest, cuff, SuiteItem{Sample: true, SampleArea: "背后"}, SuiteItem{Sample: true, SampleArea: "帽子"}),
		"unknown area":        areaPlan(chest, cuff, SuiteItem{UseSamples: []string{"背后"}}),
		"sample uses sample":  areaPlan(chest, SuiteItem{Sample: true, SampleArea: "袖口", UseSamples: []string{"胸前"}}),
		"no room for samples": areaPlan(chest, cuff, SuiteItem{UseSamples: []string{"胸前", "袖口"}, References: []string{"a", "b", "c"}}),
	} {
		if e := s.validatePlan(&r, &p); e == nil {
			t.Fatal("plan accepted:", name)
		}
	}
	ok := areaPlan(chest, cuff, SuiteItem{UseSamples: []string{"胸前", "袖口"}}, SuiteItem{UseSamples: []string{"胸前"}}, SuiteItem{UseSample: true}, SuiteItem{})
	if e := s.validatePlan(&r, &ok); e != nil {
		t.Fatal(e)
	}
	chestID, cuffID := ok.Items[0].ID, ok.Items[1].ID
	for i, want := range [][]string{nil, nil, {chestID, cuffID}, {chestID}, {chestID, cuffID}, nil} {
		if got := samplesUsed(ok, ok.Items[i]); fmt.Sprint(got) != fmt.Sprint(want) {
			t.Errorf("item %d uses %v, want %v", i, got, want)
		}
	}
	// The main design's sample is a master; a cuff sample keeps the close-up rule.
	if !strings.Contains(sampleLead("胸前"), "设计母版") || sampleLead("袖口") != sampleSelfLine || sampleLead("") != sampleSelfLine {
		t.Fatal("sample leads")
	}
}

// A submitted set links each image to the samples of the areas it shows.
func TestSubmittedSuiteLinksImagesToTheirAreaSamples(t *testing.T) {
	s := testService(t)
	llm := saveLLM(t, s, "https://example.com/anthropic")
	images := saveConnection(t, s, "https://example.com/v1")
	s.agentRunner = func(ctx context.Context, in agentInvocation) error {
		r := invocationRun(t, in)
		p := simplePlan(12, []string{})
		p.Items[0].Sample, p.Items[0].SampleArea = true, "胸前"
		p.Items[1].Sample, p.Items[1].SampleArea = true, "袖口"
		p.Items[2].UseSamples = []string{"胸前", "袖口"}
		p.Items[3].UseSamples = []string{"胸前"}
		if e := saveRunnerPlan(s, r, p); e != nil {
			return e
		}
		emitAgentSuccess(in)
		return nil
	}
	id, e := s.StartSmartGeneration(agentInput(llm, images), "area-samples")
	if e != nil {
		t.Fatal(e)
	}
	s.waitAgent()
	tasks, _ := s.GetAgentTasks(id)
	if len(tasks) != 12 {
		t.Fatal(len(tasks))
	}
	chest, cuff := tasks[0], tasks[1]
	if !strings.HasPrefix(chest.Prompt, sampleLead("胸前")) || chest.SampleArea != "胸前" || chest.Quality != sampleQuality || cuff.SampleArea != "袖口" || cuff.Quality != sampleQuality {
		t.Fatalf("samples: %+v / %+v", chest, cuff)
	}
	if fmt.Sprint(tasks[2].SampleItems) != fmt.Sprint([]string{chest.PlanItemID, cuff.PlanItemID}) || tasks[2].SampleItem != chest.PlanItemID || !tasks[2].SamplePending {
		t.Fatalf("both samples not linked: %+v", tasks[2])
	}
	if fmt.Sprint(tasks[3].SampleItems) != fmt.Sprint([]string{chest.PlanItemID}) || tasks[4].SamplePending {
		t.Fatalf("area links wrong: %+v / %+v", tasks[3], tasks[4])
	}
}

// An image waits for all its samples and gets them first, each named by area;
// a failed sample is left out.
func TestImageGetsAllItsAreaSamplesFirst(t *testing.T) {
	sample := func(item, area, status, check string) Task {
		return Task{ID: NewID(), AgentRunID: "run", PlanItemID: item, CreatedAt: Now(), Status: status, Sample: true, SampleArea: area, SampleCheck: check, AssetIDs: []string{item + "-img"}}
	}
	image := func() Task {
		return Task{AgentRunID: "run", Prompt: "图1=版型参考", References: []string{"shape"}, SampleItems: []string{"chest", "cuff"}, SampleItem: "chest", SamplePending: true}
	}
	waiting := image()
	if attachSample(&waiting, []Task{sample("chest", "胸前", "succeeded", "checking"), sample("cuff", "袖口", "succeeded", "passed")}) {
		t.Fatal("went ahead while the chest sample was being checked")
	}
	ready := image()
	if !attachSample(&ready, []Task{sample("chest", "胸前", "succeeded", "passed"), sample("cuff", "袖口", "succeeded", "passed")}) {
		t.Fatal("did not go ahead")
	}
	if fmt.Sprint(ready.References) != "[chest-img cuff-img shape]" || fmt.Sprint(ready.SampleAssetIDs) != "[chest-img cuff-img]" || ready.SampleAssetID != "chest-img" {
		t.Fatalf("references: %v", ready.References)
	}
	if want := samplesPromptLine([]string{"胸前", "袖口"}, 3); !strings.HasPrefix(ready.Prompt, want) || !strings.Contains(want, "第 1 张是「胸前」设计的样板，第 2 张是「袖口」设计的样板") || !strings.Contains(want, "“图1”到“图1”，依次指第 3 到第 3 张") {
		t.Fatalf("prompt: %q", ready.Prompt)
	}
	partly := image()
	attachSample(&partly, []Task{sample("chest", "胸前", "failed", ""), sample("cuff", "袖口", "succeeded", "passed")})
	if fmt.Sprint(partly.References) != "[cuff-img shape]" || !strings.HasPrefix(partly.Prompt, samplesPromptLine([]string{"袖口"}, 2)) {
		t.Fatalf("failed sample not left out: %v %q", partly.References, partly.Prompt)
	}
}

func TestRefinementCarriesAllSamples(t *testing.T) {
	p := refinePrompt("原始提示词", "背景更亮", 2)
	if !strings.Contains(p, fmt.Sprintf(refineSamplesLine, 3)) || strings.Contains(p, refineSampleLine) {
		t.Fatal(p)
	}
	if got := sampleAssetsOf(Task{SampleAssetID: "old"}); fmt.Sprint(got) != "[old]" {
		t.Fatal(got)
	}
}
