package core

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"
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
		return "", errors.New("请求标识无效")
	}
	if in.Instruction == "" {
		return "", errors.New("请填写想怎么调整这张图片")
	}
	if utf8.RuneCountInString(in.Instruction) > 1000 {
		return "", errors.New("调整说明最多1000个字")
	}
	// A repeated click (same request ID) returns the earlier submission unchanged.
	var submitted string
	if s.store.db.QueryRow(`SELECT batch FROM submissions WHERE id=?`, in.RequestID).Scan(&submitted) == nil {
		return submitted, nil
	}
	var source Task
	if e := s.store.Get("task", in.TaskID, &source); e != nil {
		return "", errors.New("找不到这张图片的任务")
	}
	if source.AgentRunID == "" || source.PlanItemID == "" {
		return "", errors.New("只能优化智能生图的结果")
	}
	if source.Status != "succeeded" {
		return "", errors.New("只能优化已完成的图片")
	}
	owned := false
	for _, id := range source.AssetIDs {
		owned = owned || id == in.AssetID
	}
	if !owned {
		return "", errors.New("图片不属于这个任务")
	}
	var asset Asset
	if e := s.store.Get("asset", in.AssetID, &asset); e != nil || asset.TaskID != source.ID {
		return "", errors.New("原图不可用，请刷新后重试")
	}
	tasks, e := List[Task](s.store, "task")
	if e != nil {
		return "", e
	}
	for _, t := range tasks {
		if t.AgentRunID == source.AgentRunID && t.PlanItemID == source.PlanItemID && active(t.Status) {
			return "", errors.New("这张图片正在生成或优化，请等待完成")
		}
	}
	var c Connection
	if e := s.store.Get("connection", source.Connection.ID, &c); e != nil {
		return "", errors.New("原生图连接已删除，请重新配置后再优化")
	}
	if normalizeConnection(c).Kind != "image" {
		return "", errors.New("请选择生图中转站")
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
	t.RefinedFrom = source.ID
	t.RefineInstruction = in.Instruction
	t.PreviousAssetIDs = nil
	t.Prompt = refinePrompt(baseTaskPrompt(source), in.Instruction)
	batch, e := s.store.Submit(in.RequestID, source.BatchID, []Task{t})
	if e != nil {
		return "", e
	}
	// The instruction is a prompt and is not logged.
	_ = s.store.appendLog("info", "agent", source.AgentRunID, "已提交图片优化请求（基于已完成图片，原图保留）")
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
	ctx, cancel := context.WithTimeout(s.ctx, 15*time.Second)
	defer cancel()
	ids, e := s.provider.Models(ctx, normalizeConnection(c), key)
	if e != nil {
		return nil, e
	}
	sort.Strings(ids)
	return ids, nil
}

const refinePromptMarker = "\n\n原图制作要求（仅用于保持一致，不要重新构图）：\n"

// baseTaskPrompt drops the refine wrapper of earlier rounds so prompts do not nest.
func baseTaskPrompt(t Task) string {
	if t.RefinedFrom != "" {
		if _, base, ok := strings.Cut(t.Prompt, refinePromptMarker); ok {
			return base
		}
	}
	return t.Prompt
}

func refinePrompt(base, instruction string) string {
	return "以提供的图片为底图做局部微调。只修改下面的调整要求，其余内容保持完全一致：商品造型与比例、图案、文字内容、字体、文字方向与排列、颜色、人物、背景和构图都不要变化。\n调整要求：" + instruction + refinePromptMarker + base
}
