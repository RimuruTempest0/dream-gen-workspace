package core

import "fmt"

// attachSample resolves the detail sample t waits for. It reports false while the
// sample's current attempt is still queued, generating or paused. Once the sample
// succeeded, its image becomes t's last reference and the prompt says how to use
// it; if the sample failed, t goes ahead without it, so its prompt (which never
// mentions the sample) stays correct.
func attachSample(t *Task, tasks []Task) bool {
	if !t.SamplePending {
		return true
	}
	var sample *Task
	for i := range tasks {
		c := &tasks[i]
		if c.AgentRunID == t.AgentRunID && c.PlanItemID == t.SampleItem && (sample == nil || c.CreatedAt > sample.CreatedAt) {
			sample = c
		}
	}
	if sample != nil {
		switch sample.Status {
		case "queued", "submitting", "running", "downloading", "blocked":
			return false
		case "succeeded":
			if len(sample.AssetIDs) > 0 && len(t.References) < 4 {
				t.References = append(t.References, sample.AssetIDs[0])
				t.SampleAssetID = sample.AssetIDs[0]
				t.Prompt = samplePromptLine(len(t.References)) + t.Prompt
			}
		}
	}
	t.SamplePending = false
	t.UpdatedAt = Now()
	return true
}

func samplePromptLine(n int) string {
	return fmt.Sprintf("样板图：参考图第 %d 张（最后一张）是本套已经生成好的细节样板。画面里出现样板中的细节（例如袖口的图标和文字）时，照样板画：排列方式、左右顺序、文字方向、字体、颜色和相对大小都与样板一致。只照样板画这部分细节，不照它的构图、背景、姿势或衣服颜色。\n", n)
}
