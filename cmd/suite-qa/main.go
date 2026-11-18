// suite-qa runs one smart suite end to end with real services, the way the
// "智能套图" page does, on a copy of a data folder: the copy keeps the saved
// connections and DPAPI-encrypted keys (same Windows user) while nothing is
// written back to the source. It generates real, billed images.
//
//	go run ./cmd/suite-qa -source <data dir> -work <new dir> -prompt <file> [-url <product link>] [-count 10]
//
// It prints the run's progress and exits 0 only when every one of -count plan
// slots ends with a saved image.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"imageworkbench/internal/core"
)

func main() {
	source := flag.String("source", "", "data folder to copy (never written)")
	work := flag.String("work", "", "new folder for the copy")
	promptFile := flag.String("prompt", "", "file holding the instruction")
	productURL := flag.String("url", "", "product link when the instruction has several")
	count := flag.Int("count", 10, "images in the suite")
	program := flag.String("program", "", "release folder with agent/, product-browser.exe and runtime/")
	limit := flag.Duration("timeout", 75*time.Minute, "give up after")
	flag.Parse()
	if *source == "" || *work == "" || *promptFile == "" || *program == "" {
		flag.Usage()
		os.Exit(2)
	}
	if _, e := os.Stat(*work); e == nil {
		fail("work folder already exists: %s", *work)
	}
	if e := copyData(*source, *work); e != nil {
		fail("copy data: %v", e)
	}
	prompt, e := os.ReadFile(*promptFile)
	if e != nil {
		fail("%v", e)
	}
	s, e := core.NewService(*work)
	if e != nil {
		fail("open data: %v", e)
	}
	s.SetAgentExecutable(filepath.Join(*program, "agent", "claude-code", "claude.exe"))
	s.SetProductBrowser(filepath.Join(*program, "product-browser.exe"), filepath.Join(*program, "runtime"))
	s.Start(func() {})
	defer s.Close()

	in, e := s.GetAgentDraft()
	if e != nil {
		fail("draft: %v", e)
	}
	in.Instruction = strings.TrimSpace(string(prompt))
	in.ProductURL = *productURL
	in.TargetCount = *count
	in.References = []string{}
	in.ProductText = ""
	in.AutoMode = s.SmartMode() == "auto"
	id, e := s.StartSmartGeneration(in, core.NewID())
	if e != nil {
		fail("start: %v", e)
	}
	log("run %s started (auto mode %v, %d images, LLM %s, image %s %s)", id, in.AutoMode, in.TargetCount, in.LLMConnectionID, in.ImageConnectionID, in.ImageModel)

	deadline := time.Now().Add(*limit)
	lastRun, lastSlots, events := "", "", 0
	for time.Now().Before(deadline) {
		time.Sleep(5 * time.Second)
		r, e := s.GetAgentRun(id)
		if e != nil {
			fail("run: %v", e)
		}
		for ; events < len(r.Events); events++ {
			log("  event: %s", r.Events[events].Text)
		}
		if now := r.Status + " / " + r.Stage; now != lastRun {
			log("run: %s", now)
			lastRun = now
		}
		switch r.Status {
		case "needs_input":
			fail("run asks for input: %+v", r.Questions)
		case "failed", "interrupted":
			fail("run %s: %s", r.Status, r.Error)
		}
		if r.BatchID == "" {
			continue
		}
		tasks, e := s.GetAgentTasks(id)
		if e != nil {
			fail("tasks: %v", e)
		}
		slots := current(tasks)
		summary := describe(slots)
		if summary != lastSlots {
			log("images: %s", summary)
			lastSlots = summary
		}
		if !settled(slots) {
			continue
		}
		done := 0
		for _, t := range slots {
			if t.Status == "succeeded" && len(t.AssetIDs) > 0 {
				done++
			} else {
				log("  slot %s ended %s: %s", t.PlanItemID[:8], t.Status, t.Error)
			}
		}
		log("finished: %d of %d slots have an image (%d slots planned); images in %s", done, *count, len(slots), filepath.Join(*work, "images"))
		if done == *count && len(slots) == *count {
			os.Exit(0)
		}
		os.Exit(1)
	}
	fail("gave up after %s", *limit)
}

// current keeps the newest attempt of each plan slot.
func current(tasks []core.Task) []core.Task {
	sort.Slice(tasks, func(i, j int) bool { return tasks[i].CreatedAt > tasks[j].CreatedAt })
	seen, out := map[string]bool{}, []core.Task{}
	for _, t := range tasks {
		if !seen[t.PlanItemID] {
			seen[t.PlanItemID] = true
			out = append(out, t)
		}
	}
	return out
}

func settled(slots []core.Task) bool {
	for _, t := range slots {
		switch t.Status {
		case "succeeded", "failed", "canceled", "download_failed":
		case "unknown":
			if t.AutoRetriedAs != "" {
				return false
			}
		default: // queued, submitting, running, downloading, blocked
			return false
		}
		if t.Status == "succeeded" && t.SampleCheck == "checking" {
			return false
		}
	}
	return len(slots) > 0
}

func describe(slots []core.Task) string {
	n := map[string]int{}
	for _, t := range slots {
		n[t.Status]++
	}
	keys := []string{}
	for k := range n {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := []string{}
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s %d", k, n[k]))
	}
	return strings.Join(parts, ", ")
}

// copyData copies the database, keys, skills and references; generated images,
// caches and the product browser profile stay behind.
func copyData(src, dst string) error {
	skip := map[string]bool{"images": true, "thumbnails": true, "agent-cache": true, "product-browser": true}
	return filepath.Walk(src, func(path string, info os.FileInfo, e error) error {
		if e != nil {
			return e
		}
		rel, _ := filepath.Rel(src, path)
		top := strings.Split(rel, string(filepath.Separator))[0]
		if skip[top] {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o700)
		}
		data, e := os.ReadFile(path)
		if e != nil {
			return e
		}
		return os.WriteFile(target, data, 0o600)
	})
}

func log(format string, v ...any) {
	fmt.Printf("%s "+format+"\n", append([]any{time.Now().Format("15:04:05")}, v...)...)
}

func fail(format string, v ...any) {
	log("FAIL: "+format, v...)
	os.Exit(1)
}
