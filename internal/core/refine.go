package core

import (
	"imageworkbench/internal/i18n"
	"context"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"
	"unicode/utf8"
)

type RefineInput struct {
	RequestID   string `json:"requestID"`
	TaskID      string `json:"taskID"`
	AssetID     string `json:"assetID"`
	Instruction string `json:"instruction"`
}

// RefineAgentImage queues one image-edit request that adjusts an existing smart
// generation result. It is a new, possibly billable request: the caller confirms
// it, the request ID makes repeated clicks idempotent, and the original image and
// task are kept. The new task joins the same slot, so it becomes the current version.
func (s *Service) RefineAgentImage(in RefineInput) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	in.Instruction = strings.TrimSpace(in.Instruction)
	if in.RequestID == "" || len(in.RequestID) > 128 {
		return "", s.fail("agent.requestInvalid")
	}
	if in.Instruction == "" {
		return "", s.fail("agent.refine.instruction")
	}
	if utf8.RuneCountInString(in.Instruction) > 1000 {
		return "", s.fail("agent.refine.tooLong")
	}
	// A repeated click (same request ID) returns the earlier submission unchanged.
	var submitted string
	if s.store.db.QueryRow(`SELECT batch FROM submissions WHERE id=?`, in.RequestID).Scan(&submitted) == nil {
		return submitted, nil
	}
	var source Task
	if e := s.store.Get("task", in.TaskID, &source); e != nil {
		return "", s.fail("agent.image.noTask")
	}
	if source.AgentRunID == "" || source.PlanItemID == "" {
		return "", s.fail("agent.refine.notCreation")
	}
	if source.Status != "succeeded" {
		return "", s.fail("agent.refine.notDone")
	}
	owned := false
	for _, id := range source.AssetIDs {
		owned = owned || id == in.AssetID
	}
	if !owned {
		return "", s.fail("agent.image.otherTask")
	}
	var asset Asset
	if e := s.store.Get("asset", in.AssetID, &asset); e != nil || asset.TaskID != source.ID {
		return "", s.fail("agent.image.unavailable")
	}
	tasks, e := List[Task](s.store, "task")
	if e != nil {
		return "", e
	}
	for _, t := range tasks {
		if t.AgentRunID == source.AgentRunID && t.PlanItemID == source.PlanItemID && active(t.Status) {
			return "", s.fail("agent.refine.busy")
		}
	}
	var c Connection
	if e := s.store.Get("connection", source.Connection.ID, &c); e != nil {
		return "", s.fail("agent.refine.connectionGone")
	}
	if normalizeConnection(c).Kind != "image" {
		return "", s.fail("agent.chooseImage")
	}
	if _, e := s.key(c.Credential); e != nil {
		return "", e
	}
	t := source
	t.ID = NewID()
	t.CreatedAt = Now()
	t.UpdatedAt = t.CreatedAt
	t.Status = "queued"
	t.Error = ""
	t.Results = nil
	t.AssetIDs = []string{}
	t.Diagnostic = GenerationDiagnostic{}
	t.Connection = c
	t.References = []string{in.AssetID}
	// The suite's samples go along after the image, so the edit has pictures of the
	// designs to draw instead of only words.
	t.SampleAssetID, t.SampleAssetIDs = "", nil
	for _, id := range sampleAssetsOf(source) {
		var sample Asset
		if len(t.References) < 4 && s.store.Get("asset", id, &sample) == nil {
			t.References = append(t.References, id)
			t.SampleAssetIDs = append(t.SampleAssetIDs, id)
		}
	}
	if len(t.SampleAssetIDs) > 0 {
		t.SampleAssetID = t.SampleAssetIDs[0]
	} else {
		t.SampleItem, t.SampleItems = "", nil
	}
	t.AutoRetryOf, t.AutoRetriedAs, t.Trigger = "", "", triggerRefine
	t.PolicyRetryOf, t.PolicyRetriedAs, t.PolicyStep, t.Rewording, t.RewordFor = "", "", 0, false, ""
	t.SampleCheck, t.SampleProblems = "", ""
	t.RefinedFrom = source.ID
	t.RefineInstruction = in.Instruction
	t.PreviousAssetIDs = nil
	t.Language = s.Language() // a new request: the language chosen now
	t.Prompt = refinePrompt(baseTaskPrompt(source), in.Instruction, len(t.SampleAssetIDs), t.Language)
	// The original requirements often state the opposite of the change ("only the
	// middle stocking has the design"); outnumbered, the change was ignored. With
	// an LLM line they are first rewritten to agree with it (nothing sent meanwhile).
	// The same LLM call decides whether the change is made on the old image or the
	// image is drawn again (refineRequirements); without an LLM line it is an edit.
	held := false
	t.RefineMode, t.RefineUnjudged = "", false
	if _, _, ok := s.rewordLLMLocked(t); ok {
		t.Rewording, t.RewordFor, held = true, rewordRefine, true
	} else {
		t.RefineMode, t.RefineUnjudged = refineEdit, true
	}
	if e := s.ensureCanGenerateLocked(1, c.ID); e != nil {
		return "", e
	}
	t.Trial = s.trialLocked()
	batch, e := s.store.Submit(in.RequestID, source.BatchID, []Task{t})
	if e != nil {
		return "", e
	}
	// The instruction is a prompt and is not logged.
	_ = s.store.appendLog("info", "agent", source.AgentRunID, s.msg("agent.log.refineSubmitted"))
	if held {
		s.startRewordLocked(t.ID)
	}
	s.signal()
	return batch, nil
}

// ListConnectionModels returns the model IDs of a saved connection for selection
// lists. It only performs GET /models with the stored key: no generation, no cost.
func (s *Service) ListConnectionModels(id string) ([]string, error) {
	s.mu.Lock()
	var c Connection
	e := s.store.Get("connection", id, &c)
	var key string
	if e == nil {
		key, e = s.key(c.Credential)
	}
	s.mu.Unlock()
	if e != nil {
		return nil, e
	}
	// Same limit as the connection test: some relays answer /models slowly.
	ctx, cancel := context.WithTimeout(s.ctx, probeTimeout)
	defer cancel()
	ids, e := s.provider.Models(ctx, normalizeConnection(c), key)
	if e != nil {
		// Fall back to the last list this relay returned (connection test, a
		// health check or an earlier lookup), so the picker is not left empty.
		if saved := s.savedModels(id); len(saved) > 0 {
			return saved, nil
		}
		return nil, e
	}
	sort.Strings(ids)
	s.saveModels(id, "", ids)
	return ids, nil
}

const modelListKind = "connection_models"

func (s *Service) savedModels(id string) []string {
	var ids []string
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.store.Get(modelListKind, id, &ids) != nil || len(ids) == 0 {
		if p, ok := s.probes[id]; ok && p.ok {
			ids = append([]string{}, p.models...)
			sort.Strings(ids)
		}
	}
	return ids
}

// saveModels remembers a relay's model list; it never holds a key. A non-empty
// base must match the saved connection (a test of edited, unsaved fields is skipped).
func (s *Service) saveModels(id, base string, ids []string) {
	if len(ids) == 0 {
		return
	}
	sorted := append([]string{}, ids...)
	sort.Strings(sorted)
	s.mu.Lock()
	defer s.mu.Unlock()
	var c Connection
	if s.store.Get("connection", id, &c) == nil && (base == "" || c.BaseURL == base) {
		_ = s.store.Put(modelListKind, id, sorted)
	}
}

const refinePromptMarker = "\n\n原图制作要求（只用于保持没有要求修改的部分；与本次修改冲突的内容作废，不要重新构图）：\n"

// refinePromptMarkerV1 is the marker of refine prompts saved before 0.6.11.
const refinePromptMarkerV1 = "\n\n原图制作要求（仅用于保持一致，不要重新构图）：\n"

// baseTaskPrompt drops the refine wrapper of earlier rounds so prompts do not nest.
func baseTaskPrompt(t Task) string {
	if t.RefinedFrom != "" {
		for _, marker := range refineMarkers {
			if _, base, ok := strings.Cut(t.Prompt, marker); ok {
				return base
			}
		}
	}
	return t.Prompt
}

// refinePrompt puts the requested change ahead of everything that must stay. The
// original prompt still describes the old state (and earlier wrappers listed text
// arrangement as unchangeable), so without an explicit priority the edit model
// keeps the image as it was. The original's sample line points at a reference
// position the edit no longer has; left in, the model took the image being edited
// as the sample and copied its cuff unchanged, so it is replaced by withSample's line.
func refinePrompt(base, instruction string, samples int, lang string) string {
	base = refineBase(base)
	if !zhPrompt(lang) {
		head := enRefineHead + instruction + enRefineHeadRest
		switch {
		case samples == 1:
			head += "\n" + enRefineSampleLine
		case samples > 1:
			head += "\n" + fmt.Sprintf(enRefineSamplesLine, samples+1)
		}
		return head + enRefinePromptMarker + base
	}
	head := "以提供的第 1 张图片为底图修改。\n本次修改（必须做到，改完后要能明显看出变化）：" + instruction + "\n本次修改优先于原图现状和下面的原图制作要求，二者冲突时按本次修改画。为做到本次修改，可以一并调整与它直接相关的局部，例如要把袖口文字摆正时，把有刺绣的手臂和袖子改成在画面里竖直下垂、袖口接缝水平。除此以外保持完全一致：商品造型与比例、图案、文字内容、字体、颜色、人物、背景和构图都不要变化。"
	switch {
	case samples == 1:
		head += "\n" + refineSampleLine
	case samples > 1:
		head += "\n" + fmt.Sprintf(refineSamplesLine, samples+1)
	}
	return head + refinePromptMarker + base
}

// refineBase is the original prompt as a refinement restates it.
func refineBase(base string) string {
	// Prompts saved before 0.6.13 told the model to tilt cuff text with the sleeve,
	// which argues against straightening it; use the current wording instead.
	base = strings.ReplaceAll(base, p004TiltWording, p004LevelWording)
	if hasSampleLead(base) {
		if _, rest, ok := strings.Cut(base, "\n"); ok {
			base = rest
		}
	}
	return base
}

// ---- requirements that agree with the change (0.7.6) --------------------------

const rewordRefine = "refine"

const refineMergeInstruction = `下面是一张电商图片原来的制作要求，用户要在这张已生成的图上做一处修改。请改写这份制作要求，让它与修改完全一致：
1. 找出所有与修改冲突的句子，不论在哪一行（主体、构图、画面文字、本张变化、约束、避免、统一锁定项、项目固定规则等），都改成符合修改后的描述。例如修改要求三只袜子都带刺绣，就把“只有中间一只带刺绣”“左右两只没有图案”“带刺绣的只有这一只”之类的说法都改成三只都带。
2. 与修改无关的要求原样保留，包括引号里的画面文字、尺寸数字、参考图编号和作用、固定规则。修改需要新的画面文字时才加，并照用户写的逐字使用。
3. 不新增修改以外的内容，不解释。
4. 判断这次修改的做法，写在第一行，只写一个标记：修改涉及人物（换模特、换人）、姿势、场景或展示方式（例如改成平铺、挂拍、礼盒）时写 [regenerate]，这张图会按改写后的制作要求重新生成；其他局部调整（例如亮一点、摆正袖口、改一处文字）写 [edit]，在原图上改。
5. 从第二行起只输出改写后的完整制作要求，保持原来的分行格式，不要标题、说明或代码块。

用户的修改：%s

原制作要求：
`

// How a refinement is made (Task.RefineMode).
const (
	refineEdit       = "edit"
	refineRegenerate = "regenerate"
)

var refineModeLine = regexp.MustCompile(`(?i)^\W*(regenerate|edit)\W*$`)

// splitRefineMode takes the LLM's decision off the first line of its reply; an
// empty mode means the reply did not say.
func splitRefineMode(text string) (mode, rest string) {
	first, rest, _ := strings.Cut(strings.TrimSpace(text), "\n")
	if m := refineModeLine.FindStringSubmatch(strings.TrimSpace(first)); m != nil {
		return strings.ToLower(m[1]), strings.TrimSpace(rest)
	}
	return "", strings.TrimSpace(text)
}

var removalWords = regexp.MustCompile(`去掉|删除|删掉|不要|取消|拿掉`)

// refineRequirements rewrites a refinement's original requirements so they no
// longer contradict the change. The same call decides how the change is made;
// the mode is empty when the reply did not say.
func (s *Service) refineRequirements(ctx context.Context, c Connection, key string, t Task) (merged, mode string, err error) {
	base := refineBase(baseTaskPrompt(t))
	text, e := s.llmRewrite(ctx, c, key, withPromptLanguage(t.Language, fmt.Sprintf(refineMergeInstruction, t.RefineInstruction)+base), s.msg("agent.llmStage.refineMerge"), func(text string) error {
		_, rest := splitRefineMode(text)
		return s.localErr(checkRefineRequirements(base, t.RefineInstruction, rest))
	})
	if e != nil {
		return "", "", e
	}
	mode, merged = splitRefineMode(text)
	return merged, mode, nil
}

// applyRefineLocked builds a held refinement from its rewritten requirements. A
// regeneration draws the image again: the plan's references and the samples
// (attached when it is sent, as for the set's own images), never the old image.
// An edit keeps the old image first. An undecided change is an edit.
func (s *Service) applyRefineLocked(t *Task, merged, mode string) {
	if mode != refineRegenerate {
		t.RefineMode, t.RefineUnjudged = refineEdit, mode == ""
		t.Prompt = refinePrompt(merged, t.RefineInstruction, len(sampleAssetsOf(*t)), t.Language)
		return
	}
	t.RefineMode, t.RefineUnjudged = refineRegenerate, false
	t.References = s.planReferencesLocked(*t)
	t.SampleAssetID, t.SampleAssetIDs = "", nil
	t.SamplePending = t.SampleItem != "" || len(t.SampleItems) > 0
	t.Prompt = regeneratePrompt(merged, t.RefineInstruction, t.Language)
}

// planReferencesLocked is the reference pictures the plan gave t's slot.
func (s *Service) planReferencesLocked(t Task) []string {
	var r AgentRun
	if s.store.Get("agent_run", t.AgentRunID, &r) != nil || r.SubmittedRevision < 1 || r.SubmittedRevision > len(r.Plans) {
		return []string{}
	}
	for _, item := range r.Plans[r.SubmittedRevision-1].Items {
		if item.ID == t.PlanItemID {
			refs := []string{}
			for _, id := range item.References {
				var a Asset
				if s.store.Get("asset", id, &a) == nil {
					refs = append(refs, id)
				}
			}
			return refs
		}
	}
	return []string{}
}

// The wrapper of a regenerated refinement (English: prompt_scaffold.go).
const (
	regenerateHead     = "按下面这张图的制作要求重新生成一张新图，不在旧图上修改。\n本次修改（必须做到，生成后要能明显看出变化）："
	regenerateHeadRest = "\n本次修改优先于下面的制作要求，二者冲突时按本次修改画。商品本身（款式、颜色、面料）和各设计区的设计保持不变。"
	regenerateMarker   = "\n\n这张图的制作要求（与本次修改冲突的内容作废）：\n"
)

// regeneratePrompt is the prompt of a refinement drawn again from its requirements.
func regeneratePrompt(base, instruction, lang string) string {
	if !zhPrompt(lang) {
		return enRegenerateHead + instruction + enRegenerateHeadRest + enRegenerateMarker + base
	}
	return regenerateHead + instruction + regenerateHeadRest + regenerateMarker + base
}

// checkRefineRequirements guards the exact in-image text: the rewrite may not
// invent text, and keeps every original text unless the change replaces it with
// text the user wrote or asks to remove it.
func checkRefineRequirements(base, instruction, text string) error {
	if utf8.RuneCountInString(text) < utf8.RuneCountInString(base)/3 {
		return i18n.Fail("agent.rewrite.tooShort")
	}
	quoted := func(v string) []string {
		out := []string{}
		for _, q := range quotedText.FindAllStringSubmatch(v, -1) {
			out = append(out, q[1]+q[2])
		}
		return out
	}
	original, got := quoted(base), quoted(text)
	replaced := false
	for _, g := range got {
		switch {
		case slices.Contains(original, g):
		case strings.Contains(instruction, g):
			replaced = true
		default:
			return i18n.Fail("agent.rewrite.newText", "text", g)
		}
	}
	if replaced || removalWords.MatchString(instruction) {
		return nil
	}
	for _, o := range original {
		if !slices.Contains(got, o) {
			return i18n.Fail("agent.rewrite.lostText", "text", o)
		}
	}
	return nil
}

const refineSamplesLine = "第 2 到第 %d 张图片是本套各设计区的样板（例如胸前图案、袖口细节），不是底图。只照设计区里的设计：图案、配饰、朝向、文字的排列、字体和颜色照样板画；不照样板的构图、姿势、展示方式和场景，也不照它们的衣服颜色。"

// sampleAssetsOf lists the sample images a task was given (one before 0.7.9).
func sampleAssetsOf(t Task) []string {
	if len(t.SampleAssetIDs) > 0 {
		return t.SampleAssetIDs
	}
	if t.SampleAssetID != "" {
		return []string{t.SampleAssetID}
	}
	return nil
}

const refineSampleLine = "第 2 张图片是本套的细节样板（袖口等局部特写），不是底图。只照设计区里的设计：图标和文字的排列方式、左右顺序、文字方向、字体、颜色和相对大小照样板画；不照样板的构图、姿势、展示方式和场景，也不照它的衣服颜色。"
