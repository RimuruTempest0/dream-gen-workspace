package core

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"

	"imageworkbench/internal/i18n"
)

// A 反馈包 lets the provider (and Claude) replay one 套图 from start to end: the
// tester's verdict (反馈.md, first entry, for people), and run.json with the
// suite's input, instructions, product facts, the skill and shop-rule text the
// run used, its plans and events, and every image task: prompt, reference
// order, line name and model, status, errors, diagnostics, request IDs,
// automatic resends, refinements and versions.
//
// It is redacted like the diagnostic package (keys, credentials, query
// parameters and line addresses hidden; lines appear by name and model only)
// and never holds customer.dat. Fields an older suite did not record stay
// missing; nothing is filled in after the fact.

const (
	feedbackMarkdownName = "反馈.md"
	feedbackRunName      = "run.json"
	feedbackFormat       = 1
)

type feedbackPackage struct {
	Format     int    `json:"format"`
	AppVersion string `json:"appVersion"`
	// CustomerID is the software ID of the hosted edition's licence file.
	CustomerID string          `json:"customerID,omitempty"`
	Language   string          `json:"language"`
	ExportedAt string          `json:"exportedAt"`
	Feedback   SuiteFeedback   `json:"feedback"`
	Run        feedbackRun     `json:"run"`
	Tasks      []feedbackTask  `json:"tasks"`
}

// feedbackLine is a line as the package names it: never its address or key.
type feedbackLine struct {
	Name  string `json:"name,omitempty"`
	Model string `json:"model,omitempty"`
}

type feedbackSkill struct {
	Name     string `json:"name"`
	Source   string `json:"source,omitempty"`
	Version  int    `json:"version,omitempty"`
	Category string `json:"category,omitempty"`
	Content  string `json:"content"`
}

type feedbackRun struct {
	ID                string          `json:"id"`
	CreatedAt         string          `json:"createdAt,omitempty"`
	UpdatedAt         string          `json:"updatedAt,omitempty"`
	Status            string          `json:"status"`
	Stage             string          `json:"stage,omitempty"`
	Error             string          `json:"error,omitempty"`
	StopReason        string          `json:"stopReason,omitempty"`
	Language          string          `json:"language,omitempty"`
	Input             AgentInput      `json:"input"`
	Instructions      []string        `json:"instructions,omitempty"`
	LLMLine           *feedbackLine   `json:"llmLine,omitempty"`
	ImageLine         *feedbackLine   `json:"imageLine,omitempty"`
	AutoGeneration    bool            `json:"autoGeneration,omitempty"`
	AuthorizedCount   int             `json:"authorizedCount,omitempty"`
	AutoContinues     int             `json:"autoContinues,omitempty"`
	LLMReconnects     int             `json:"llmReconnects,omitempty"`
	Skills            []feedbackSkill `json:"skills,omitempty"`
	ShopRules         []ShopRule      `json:"shopRules,omitempty"`
	Product           ProductInfo     `json:"product"`
	Questions         []AgentQuestion `json:"questions,omitempty"`
	Crops             []string        `json:"crops,omitempty"`
	Plans             []SuitePlan     `json:"plans,omitempty"`
	SubmittedRevision int             `json:"submittedRevision,omitempty"`
	BatchID           string          `json:"batchID,omitempty"`
	Events            []AgentEvent    `json:"events,omitempty"`
	Activity          []ActivityEntry `json:"activity,omitempty"`
}

type feedbackDiagnostic struct {
	Stage             string `json:"stage,omitempty"`
	StartedAt         string `json:"startedAt,omitempty"`
	HTTPStatus        int    `json:"httpStatus,omitempty"`
	UpstreamRequestID string `json:"upstreamRequestID,omitempty"`
	UpstreamJobID     string `json:"upstreamJobID,omitempty"`
	ElapsedMS         int64  `json:"elapsedMS,omitempty"`
	BytesReceived     int    `json:"bytesReceived,omitempty"`
	NetworkError      string `json:"networkError,omitempty"`
	DownloadMS        int64  `json:"downloadMS,omitempty"`
}

type feedbackTask struct {
	ID                string              `json:"id"`
	PlanItemID        string              `json:"planItemID,omitempty"`
	CreatedAt         string              `json:"createdAt,omitempty"`
	UpdatedAt         string              `json:"updatedAt,omitempty"`
	Status            string              `json:"status"`
	Error             string              `json:"error,omitempty"`
	Language          string              `json:"language,omitempty"`
	Prompt            string              `json:"prompt"`
	References        []string            `json:"references,omitempty"`
	Line              *feedbackLine       `json:"line,omitempty"`
	Model             string              `json:"model,omitempty"`
	Size              string              `json:"size,omitempty"`
	Quality           string              `json:"quality,omitempty"`
	AssetIDs          []string            `json:"assetIDs,omitempty"`
	Diagnostic        *feedbackDiagnostic `json:"diagnostic,omitempty"`
	RoutedFromName    string              `json:"routedFromName,omitempty"`
	RouteReason       string              `json:"routeReason,omitempty"`
	AutoRetryOf       string              `json:"autoRetryOf,omitempty"`
	AutoRetriedAs     string              `json:"autoRetriedAs,omitempty"`
	Trigger           string              `json:"trigger,omitempty"`
	PolicyRetryOf     string              `json:"policyRetryOf,omitempty"`
	PolicyRetriedAs   string              `json:"policyRetriedAs,omitempty"`
	PolicyStep        int                 `json:"policyStep,omitempty"`
	RefinedFrom       string              `json:"refinedFrom,omitempty"`
	RefineInstruction string              `json:"refineInstruction,omitempty"`
	RefineMode        string              `json:"refineMode,omitempty"`
	RefineUnjudged    bool                `json:"refineUnjudged,omitempty"`
	Sample         bool                `json:"sample,omitempty"`
	SampleArea        string              `json:"sampleArea,omitempty"`
	SampleItems       []string            `json:"sampleItems,omitempty"`
	SampleAssetIDs    []string            `json:"sampleAssetIDs,omitempty"`
	SampleCheck       string              `json:"sampleCheck,omitempty"`
	SampleProblems    string              `json:"sampleProblems,omitempty"`
	SampleRedraws     int                 `json:"sampleRedraws,omitempty"`
	Trial             bool                `json:"trial,omitempty"`
}

// FeedbackPackageFile is one entry of the 反馈包, as listed before saving.
type FeedbackPackageFile struct {
	Name  string `json:"name"`
	Bytes int64  `json:"bytes"`
}

// FeedbackPackagePreview lists what the 反馈包 will hold.
type FeedbackPackagePreview struct {
	Files []FeedbackPackageFile `json:"files"`
	Bytes int64                 `json:"bytes"`
}

// PreviewFeedbackPackage lists the entries of the suite's 反馈包 and their size.
func (s *Service) PreviewFeedbackPackage(runID string) (FeedbackPackagePreview, error) {
	s.mu.Lock()
	entries, e := s.feedbackEntriesLocked(runID)
	s.mu.Unlock()
	out := FeedbackPackagePreview{Files: []FeedbackPackageFile{}}
	for _, v := range entries {
		out.Files = append(out.Files, FeedbackPackageFile{v.name, int64(len(v.data))})
		out.Bytes += int64(len(v.data))
	}
	return out, e
}

// ExportFeedbackPackage writes the suite's 反馈包 to destination. It does not
// use the image queue.
func (s *Service) ExportFeedbackPackage(runID, destination string) error {
	s.mu.Lock()
	entries, e := s.feedbackEntriesLocked(runID)
	s.mu.Unlock()
	if e != nil {
		return e
	}
	return writeZipFile(destination, entries)
}

// feedbackEntriesLocked puts the package together: 反馈.md first, then run.json.
func (s *Service) feedbackEntriesLocked(runID string) ([]zipEntry, error) {
	feedback, e := s.suiteFeedbackLocked(runID)
	if e != nil {
		return nil, e
	}
	var run AgentRun
	if e = s.store.Get("agent_run", runID, &run); e != nil {
		return nil, e
	}
	tasks, e := List[Task](s.store, "task")
	if e != nil {
		return nil, e
	}
	mine := []Task{}
	conns, e := List[Connection](s.store, "connection")
	if e != nil {
		return nil, e
	}
	// Lines saved on the run and its tasks are hidden too, even if since deleted.
	conns = append(conns, run.ImageConnection, run.LLMConnection)
	for _, t := range tasks {
		if t.AgentRunID == runID {
			mine = append(mine, t)
			conns = append(conns, t.Connection)
		}
	}
	sort.SliceStable(mine, func(i, j int) bool { return earlier(mine[i].CreatedAt, mine[j].CreatedAt) })
	pkg := feedbackPackage{Format: feedbackFormat, AppVersion: AppVersion, Language: s.Language(), ExportedAt: Now(), Feedback: feedback, Run: feedbackRunOf(run), Tasks: []feedbackTask{}}
	if p := s.lic.profile; p != nil {
		pkg.CustomerID = p.SoftwareID
	}
	for _, t := range mine {
		pkg.Tasks = append(pkg.Tasks, feedbackTaskOf(t))
	}
	// The package is read by the provider: hidden parts are marked in Chinese.
	redactStrings(reflect.ValueOf(&pkg), s.redactorLocked(conns, i18n.ZhCN))
	data, e := json.MarshalIndent(pkg, "", "  ")
	if e != nil {
		return nil, e
	}
	return []zipEntry{{feedbackMarkdownName, []byte(feedbackMarkdown(pkg))}, {feedbackRunName, data}}, nil
}

func lineOf(c Connection) *feedbackLine {
	if c.Name == "" && c.Model == "" {
		return nil
	}
	return &feedbackLine{Name: c.Name, Model: c.Model}
}

func feedbackRunOf(r AgentRun) feedbackRun {
	out := feedbackRun{ID: r.ID, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt, Status: r.Status, Stage: r.Stage, Error: r.Error, StopReason: r.StopReason, Language: r.Language, Input: r.Input, Instructions: r.Instructions, LLMLine: lineOf(r.LLMConnection), ImageLine: lineOf(r.ImageConnection), AutoGeneration: r.AutoGeneration, AuthorizedCount: r.AuthorizedCount, AutoContinues: r.AutoContinues, LLMReconnects: r.LLMReconnects, ShopRules: r.ShopRules, Product: r.Product, Questions: r.Questions, Crops: r.Crops, Plans: r.Plans, SubmittedRevision: r.SubmittedRevision, BatchID: r.BatchID, Events: r.Events, Activity: r.Activity}
	for _, k := range r.Skills {
		out.Skills = append(out.Skills, feedbackSkill{Name: k.Name, Source: k.Source, Version: k.Version, Category: k.Category, Content: k.Content})
	}
	return out
}

func feedbackTaskOf(t Task) feedbackTask {
	out := feedbackTask{ID: t.ID, PlanItemID: t.PlanItemID, CreatedAt: t.CreatedAt, UpdatedAt: t.UpdatedAt, Status: t.Status, Error: t.Error, Language: t.Language, Prompt: t.Prompt, References: t.References, Line: lineOf(t.Connection), Model: t.Model, Size: t.Size, Quality: t.Quality, AssetIDs: t.AssetIDs, RoutedFromName: t.RoutedFromName, RouteReason: t.RouteReason, AutoRetryOf: t.AutoRetryOf, AutoRetriedAs: t.AutoRetriedAs, Trigger: t.Trigger, PolicyRetryOf: t.PolicyRetryOf, PolicyRetriedAs: t.PolicyRetriedAs, PolicyStep: t.PolicyStep, RefinedFrom: t.RefinedFrom, RefineInstruction: t.RefineInstruction, RefineMode: t.RefineMode, RefineUnjudged: t.RefineUnjudged, Sample: t.Sample, SampleArea: t.SampleArea, SampleItems: t.SampleItems, SampleAssetIDs: t.SampleAssetIDs, SampleCheck: t.SampleCheck, SampleProblems: t.SampleProblems, SampleRedraws: t.SampleRedraws, Trial: t.Trial}
	d := t.Diagnostic
	if diag := (feedbackDiagnostic{Stage: d.Stage, StartedAt: d.StartedAt, HTTPStatus: d.HTTPStatus, UpstreamRequestID: d.UpstreamRequestID, UpstreamJobID: d.UpstreamJobID, ElapsedMS: d.ElapsedMS, BytesReceived: d.BytesReceived, NetworkError: d.NetworkError, DownloadMS: d.DownloadMS}); diag != (feedbackDiagnostic{}) {
		out.Diagnostic = &diag
	}
	return out
}

// redactStrings runs redact over every string reachable from v (a pointer).
func redactStrings(v reflect.Value, redact func(string) string) {
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface:
		if !v.IsNil() {
			redactStrings(v.Elem(), redact)
		}
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			if v.Type().Field(i).IsExported() {
				redactStrings(v.Field(i), redact)
			}
		}
	case reflect.Slice, reflect.Array:
		for i := 0; i < v.Len(); i++ {
			redactStrings(v.Index(i), redact)
		}
	case reflect.Map:
		for _, k := range v.MapKeys() {
			if x := v.MapIndex(k); x.Kind() == reflect.String {
				v.SetMapIndex(k, reflect.ValueOf(redact(x.String())).Convert(x.Type()))
			}
		}
	case reflect.String:
		if v.CanSet() {
			v.SetString(redact(v.String()))
		}
	}
}

// feedbackLabels are the fixed labels of 反馈.md. The file is written for the
// provider, so they are Chinese in every interface language.
var feedbackLabels = map[string]string{
	"usable":              "整套可用",
	"partly-usable":       "部分可用",
	"unusable":            "整套不可用",
	"missing-shot":        "少了点名要的图",
	"too-similar":         "几张图太像",
	"design-inconsistent": "设计在各图之间不一致",
	"product-changed":     "商品本身被改了",
	"edit-missed":         "改图没改到",
	"interrupted":         "生成中断或报错，要反复手动重新生成",
	"other":               "其他",
	// task and run states
	"succeeded":       "已完成",
	"failed":          "失败",
	"unknown":         "结果未知",
	"blocked":         "已暂停",
	"canceled":        "已取消",
	"queued":          "排队中",
	"submitting":      "生成中",
	"running":         "生成中",
	"downloading":     "下载中",
	"download_failed": "下载失败",
	"analyzing":       "分析中",
	"needs_input":     "等待补充信息",
	"review":          "等待确认方案",
	"ready":           "方案已就绪",
	"submitted":       "已提交生成",
	"none":            "未提交",
	// who started an attempt again (Task.Trigger)
	triggerAutoResend: "自动重发",
	triggerRetry:      "顾客单张重新生成",
	triggerRetryAll:   "顾客一键补齐（重新生成全部失败图片）",
	triggerRefine:     "顾客修改",
	"refine-regenerate": "重新生成",
	"refine-edit":       "在原图上改",
	// the sample check (Task.SampleCheck)
	"sample-checking": "样板核对中",
	"sample-passed":   "样板核对通过",
	"sample-redrawn":  "样板核对不通过，已自动重画",
	"sample-failed":   "样板重画后仍不通过，其余图照常生成",
	"sample-skipped":  "样板未能核对",
}

func feedbackLabel(code string) string {
	if v, ok := feedbackLabels[code]; ok {
		return v
	}
	return code
}

// feedbackMarkdown writes 反馈.md: the verdict first, then the request against
// the plan's images, then the timeline and errors.
func feedbackMarkdown(p feedbackPackage) string {
	var b strings.Builder
	line := func(format string, a ...any) { fmt.Fprintf(&b, format+"\n", a...) }
	quote := func(text string) {
		for _, l := range strings.Split(strings.TrimSpace(text), "\n") {
			line("> %s", l)
		}
	}
	f := p.Feedback
	line("# 问题反馈")
	line("")
	line("## 核验结论")
	line("")
	if f.Verdict == "" {
		line("- 整套判断：（未填写）")
	} else {
		line("- 整套判断：%s", feedbackLabel(f.Verdict))
	}
	if len(f.Problems) > 0 {
		names := []string{}
		for _, v := range f.Problems {
			names = append(names, feedbackLabel(v))
		}
		line("- 问题类型：%s", strings.Join(names, "；"))
	}
	if f.UpdatedAt != "" {
		line("- 填写时间：%s", f.UpdatedAt)
	}
	if strings.TrimSpace(f.Note) != "" {
		line("- 整套说明：")
		line("")
		quote(f.Note)
	}
	line("")
	b.WriteString(feedbackItemsMarkdown(p))
	line("## 这套图")
	line("")
	line("- 程序版本：%s", p.AppVersion)
	if p.CustomerID != "" {
		line("- 顾客 ID：%s", p.CustomerID)
	}
	line("- 界面语言：%s", p.Language)
	line("- 导出时间：%s", p.ExportedAt)
	r := p.Run
	line("- 套图编号：%s", r.ID)
	if r.CreatedAt != "" {
		line("- 开始时间：%s", r.CreatedAt)
	}
	line("- 状态：%s", feedbackLabel(r.Status))
	if r.Stage != "" {
		line("- 阶段：%s", r.Stage)
	}
	if r.Error != "" {
		line("- 错误：%s", r.Error)
	}
	if r.ImageLine != nil {
		line("- 生图线路：%s（%s）", r.ImageLine.Name, r.ImageLine.Model)
	}
	if r.LLMLine != nil {
		line("- AI 模型线路：%s（%s）", r.LLMLine.Name, r.LLMLine.Model)
	}
	line("")
	line("## 用户原要求")
	line("")
	quote(r.Input.Instruction)
	for i, v := range r.Instructions {
		if i == 0 && strings.TrimSpace(v) == strings.TrimSpace(r.Input.Instruction) {
			continue
		}
		line("")
		line("补充要求 %d：", i+1)
		quote(v)
	}
	line("")
	line("## 方案里的每张图")
	line("")
	if len(r.Plans) == 0 {
		line("（这套图没有保存方案）")
	} else {
		plan := submittedPlan(r)
		// The newest task of a slot is its current attempt.
		current := map[string]feedbackTask{}
		for _, t := range p.Tasks {
			current[t.PlanItemID] = t
		}
		// Each of the customer's requests (as the Agent listed them) against the
		// images carrying it out; a request no image carries out is a row of its
		// own. Plans that did not record requests or 展示方式 say so.
		row := func(no, request string, i int, item SuiteItem) {
			state := feedbackLabel("none")
			if t, ok := current[item.ID]; ok {
				state = feedbackLabel(t.Status)
			}
			sample := ""
			if item.Sample {
				sample = item.SampleArea
				if sample == "" {
					sample = "是"
				}
			}
			presentation := cell(item.Presentation)
			if presentation == "" {
				presentation = "（未记录）"
			}
			line("| %s | %s | 第 %d 张 | %s | %s | %s | %s | %s |", no, request, i+1, cell(item.Purpose), presentation, cell(item.Scene), cell(sample), state)
		}
		line("| 序号 | 用户原要求 | 图位 | 方案用途 | 展示方式 | 场景 | 样板 | 最终状态 |")
		line("| --- | --- | --- | --- | --- | --- | --- | --- |")
		for n, request := range plan.Requests {
			found := false
			for i, item := range plan.Items {
				if item.Request == n+1 {
					row(fmt.Sprint(n+1), cell(request), i, item)
					found = true
				}
			}
			if !found {
				line("| %d | %s | （方案里没有） |  |  |  |  | 未做 |", n+1, cell(request))
			}
		}
		unlinked := "（未对应原要求）"
		if len(plan.Requests) == 0 {
			unlinked = "（未记录）"
		}
		for i, item := range plan.Items {
			if item.Request < 1 || item.Request > len(plan.Requests) {
				row("—", unlinked, i, item)
			}
		}
	}
	line("")
	line("## 时间线")
	line("")
	// Each image by its slot in the plan (第 n 张（用途）), else by its task ID.
	slots := map[string]string{}
	for v := len(r.Plans) - 1; v >= 0; v-- {
		for i, item := range r.Plans[v].Items {
			if _, ok := slots[item.ID]; !ok && item.ID != "" {
				slots[item.ID] = fmt.Sprintf("第 %d 张（%s）", i+1, oneLine(item.Purpose))
			}
		}
	}
	if len(r.Plans) > 0 {
		for i, item := range submittedPlan(r).Items {
			slots[item.ID] = fmt.Sprintf("第 %d 张（%s）", i+1, oneLine(item.Purpose))
		}
	}
	events := []timelineEntry{}
	if r.CreatedAt != "" {
		events = append(events, timelineEntry{r.CreatedAt, "开始"})
	}
	for _, v := range r.Events {
		events = append(events, timelineEntry{v.At, oneLine(v.Text)})
	}
	attempts := map[string]int{}
	for _, t := range p.Tasks {
		name, ok := slots[t.PlanItemID]
		if !ok {
			name = "图片任务 " + t.ID
		}
		attempts[t.PlanItemID]++
		// Who started the attempt, as recorded; tasks stored before the trigger
		// was recorded show only what their links say.
		start := "提交生成"
		switch {
		case t.Trigger == triggerSampleRedraw:
			start = fmt.Sprintf("样板自动重画（第 %d 次）", t.SampleRedraws)
		case t.Trigger == triggerRefine || t.Trigger == "" && t.RefinedFrom != "":
			start = "顾客修改"
			if t.RefineMode != "" {
				start += "（" + feedbackLabel("refine-"+t.RefineMode) + "）"
			}
		case t.Trigger != "":
			start = feedbackLabel(t.Trigger)
		case t.AutoRetryOf != "" || t.PolicyRetryOf != "":
			start = feedbackLabel(triggerAutoResend)
		case attempts[t.PlanItemID] > 1 && t.PlanItemID != "":
			start = "重新生成（未记录触发方式）"
		}
		if t.PolicyRetryOf != "" {
			start += "（内容审核拒绝后）"
		}
		events = append(events, timelineEntry{t.CreatedAt, name + "：" + start})
		end := ""
		switch t.Status {
		case "succeeded", "unknown", "canceled":
			end = feedbackLabel(t.Status)
		case "failed", "blocked", "download_failed":
			end = feedbackLabel(t.Status)
			if t.Error != "" {
				end += "（" + oneLine(t.Error) + "）"
			}
		}
		switch t.SampleCheck {
		case "":
		case "passed", "checking":
			end = feedbackLabel("sample-" + t.SampleCheck)
		default:
			end = feedbackLabel("sample-" + t.SampleCheck)
			if t.SampleProblems != "" {
				end += "（" + oneLine(t.SampleProblems) + "）"
			}
		}
		if end != "" {
			events = append(events, timelineEntry{t.UpdatedAt, name + "：" + end})
		}
	}
	sort.SliceStable(events, func(i, j int) bool { return earlier(events[i].at, events[j].at) })
	for _, v := range events {
		line("- %s %s", v.at, v.text)
	}
	errors := []string{}
	for _, t := range p.Tasks {
		if t.Error != "" {
			errors = append(errors, fmt.Sprintf("- 图片任务 %s（%s）：%s", t.ID, feedbackLabel(t.Status), oneLine(t.Error)))
		}
	}
	if len(errors) > 0 {
		line("")
		line("## 报错")
		line("")
		for _, v := range errors {
			line("%s", v)
		}
	}
	line("")
	line("完整记录见 run.json。")
	return strings.ReplaceAll(b.String(), "\n", "\r\n")
}

func cell(text string) string { return strings.ReplaceAll(oneLine(text), "|", "/") }

// submittedPlan is the plan the suite was generated from: the submitted
// revision, else the newest. r has at least one plan.
func submittedPlan(r feedbackRun) SuitePlan {
	if r.SubmittedRevision > 0 && r.SubmittedRevision <= len(r.Plans) {
		return r.Plans[r.SubmittedRevision-1]
	}
	return r.Plans[len(r.Plans)-1]
}

// A timelineEntry is one line of the 反馈.md timeline.
type timelineEntry struct{ at, text string }

// earlier orders stored times (RFC 3339, of differing precision) by the moment
// they name; a time that does not parse is compared as text.
func earlier(a, b string) bool {
	x, e1 := time.Parse(time.RFC3339Nano, a)
	y, e2 := time.Parse(time.RFC3339Nano, b)
	if e1 != nil || e2 != nil {
		return a < b
	}
	return x.Before(y)
}
