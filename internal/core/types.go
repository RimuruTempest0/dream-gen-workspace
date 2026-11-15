package core

type Connection struct {
	// Managed is the customer-file slot ("image-1", "llm-1" …) of a line set up by
	// the licence; customers cannot edit or delete it.
	Managed         string          `json:"managed,omitempty"`
	MaxOutputTokens int             `json:"maxOutputTokens,omitempty"`
	Kind            string          `json:"kind"`
	Mode            string          `json:"mode,omitempty"`
	AuthMode        string          `json:"authMode,omitempty"`
	Capabilities    LLMCapabilities `json:"capabilities,omitempty"`
	ID              string          `json:"id"`
	Name            string          `json:"name"`
	BaseURL         string          `json:"baseURL"`
	Model           string          `json:"model"`
	Protocol        string          `json:"protocol"`
	Credential      string          `json:"credential,omitempty"`
	HasKey          bool            `json:"hasKey"`
}
type ConnectionInput struct {
	MaxOutputTokens int    `json:"maxOutputTokens"`
	Kind            string `json:"kind"`
	Mode            string `json:"mode"`
	AuthMode        string `json:"authMode"`
	ID              string `json:"id"`
	Name            string `json:"name"`
	BaseURL         string `json:"baseURL"`
	Model           string `json:"model"`
	Key             string `json:"key"`
	ClearKey        bool   `json:"clearKey"`
	// Protocol of an image connection: openai-images (default) or async-jobs.
	Protocol      string `json:"protocol,omitempty"`
	TestRequestID string `json:"testRequestID,omitempty"`
}
type GenerateInput struct {
	RequestID    string   `json:"requestID"`
	ConnectionID string   `json:"connectionID"`
	Model        string   `json:"model"`
	Prompts      []string `json:"prompts"`
	Count        int      `json:"count"`
	References   []string `json:"references"`
	Size         string   `json:"size"`
	Quality      string   `json:"quality"`
	ProjectID    string   `json:"projectID"`
	// ConceptLabels names the concept of each prompt (same index as Prompts),
	// e.g. "巨物 × 浪漫主义"; optional (0.7.18).
	ConceptLabels []string `json:"conceptLabels,omitempty"`
	// Mood is the mood guide's choice (0.7.26): each image is held while the AI
	// 模型 writes prompt, mood and the optional Concept into one prompt. The prompt
	// may then be empty. Compare draws one realistic and one painterly image.
	Mood    *MoodChoice    `json:"mood,omitempty"`
	Concept *ConceptChoice `json:"concept,omitempty"`
	Compare bool           `json:"compare,omitempty"`
}
type Task struct {
	// Concept is the concept stage label the prompt was written with (0.7.18).
	Concept string `json:"concept,omitempty"`
	// Mood is the label of the mood the image was made with ("细雨 · 深夜 · 很远 ·
	// 缓一缓"), MoodChoice and ConceptChoice what was picked, Rendering 写实 or 写意
	// (realistic/painterly, empty when not chosen) and BasePrompt the user's own
	// prompt before the rewrite (0.7.26). NudgeOf is the image a 再靠近一点 attempt
	// starts from, Nudge its direction.
	Mood          string         `json:"mood,omitempty"`
	MoodChoice    *MoodChoice    `json:"moodChoice,omitempty"`
	ConceptChoice *ConceptChoice `json:"conceptChoice,omitempty"`
	Rendering     string         `json:"rendering,omitempty"`
	BasePrompt    string         `json:"basePrompt,omitempty"`
	NudgeOf       string         `json:"nudgeOf,omitempty"`
	Nudge         string         `json:"nudge,omitempty"`
	AgentRunID    string         `json:"agentRunID,omitempty"`
	PlanItemID    string         `json:"planItemID,omitempty"`
	// RefinedFrom is the task whose image this attempt adjusts; RefineInstruction is the user's request.
	RefinedFrom       string `json:"refinedFrom,omitempty"`
	RefineInstruction string `json:"refineInstruction,omitempty"`
	// RoutedFrom is the gateway an unsent image was moved away from (no extra cost).
	RoutedFrom     string `json:"routedFrom,omitempty"`
	RoutedFromName string `json:"routedFromName,omitempty"`
	RouteReason    string `json:"routeReason,omitempty"`
	// AutoRetryOf links an automatic re-generation to the image cut by a relay
	// timeout; AutoRetriedAs is set on that original image.
	AutoRetryOf   string `json:"autoRetryOf,omitempty"`
	AutoRetriedAs string `json:"autoRetriedAs,omitempty"`
	// AutoDownloads counts automatic re-downloads of a returned result; while one
	// waits, the task stays "downloading" until DownloadRetryAt.
	AutoDownloads   int    `json:"autoDownloads,omitempty"`
	DownloadRetryAt string `json:"downloadRetryAt,omitempty"`
	// PolicyRetryOf links an automatic resend to the image the image model's content
	// check refused; PolicyRetriedAs is set on that refused image. PolicyStep is 1
	// for the unchanged resend and 2 for the reworded one. Rewording holds a queued
	// step-2 attempt (nothing sent) while the LLM rewrites its prompt.
	PolicyRetryOf   string `json:"policyRetryOf,omitempty"`
	PolicyRetriedAs string `json:"policyRetriedAs,omitempty"`
	PolicyStep      int    `json:"policyStep,omitempty"`
	Rewording       bool   `json:"rewording,omitempty"`
	// RewordFor is what the held rewrite is for: "" a refused prompt, "refine" a
	// refinement whose original requirements must first agree with the change,
	// "mood" a mood guide image whose prompt is written before it is first sent.
	RewordFor string `json:"rewordFor,omitempty"`
	// Trial marks an image requested before activation; it counts toward the trial.
	Trial bool `json:"trial,omitempty"`
	// Sample marks the suite's detail sample image. SampleItem is the plan slot of
	// that sample for an image drawn from it; SamplePending holds the image back
	// until the sample is done, and SampleAssetID is the sample image it was given
	// (empty when the sample did not succeed and the image went ahead without it).
	Sample        bool   `json:"sample,omitempty"`
	SampleItem    string `json:"sampleItem,omitempty"`
	SamplePending bool   `json:"samplePending,omitempty"`
	SampleAssetID string `json:"sampleAssetID,omitempty"`
	// Since 0.7.9 a set has one sample per design area: SampleArea names a sample's
	// area; SampleItems are all the samples an image waits for (SampleItem is the
	// first) and SampleAssetIDs the sample images it was given, in that order.
	SampleArea     string   `json:"sampleArea,omitempty"`
	SampleItems    []string `json:"sampleItems,omitempty"`
	SampleAssetIDs []string `json:"sampleAssetIDs,omitempty"`
	// SampleCheck is the LLM's check of a finished sample against its design
	// references: checking, passed, redrawn (a new attempt was queued), failed
	// (still wrong after the redraws; images go ahead) or skipped. SampleProblems
	// lists what was wrong; on a redraw attempt, what it had to correct.
	// SampleRedraws counts automatic redraws of the sample.
	SampleCheck    string `json:"sampleCheck,omitempty"`
	SampleProblems string `json:"sampleProblems,omitempty"`
	SampleRedraws  int    `json:"sampleRedraws,omitempty"`
	// PreviousAssetIDs lists images of earlier attempts in the same slot (response only, newest first).
	PreviousAssetIDs []string             `json:"previousAssetIDs,omitempty"`
	ID               string               `json:"id"`
	BatchID          string               `json:"batchID"`
	CreatedAt        string               `json:"createdAt"`
	UpdatedAt        string               `json:"updatedAt"`
	Status           string               `json:"status"`
	Error            string               `json:"error"`
	Prompt           string               `json:"prompt"`
	Model            string               `json:"model"`
	Size             string               `json:"size"`
	Quality          string               `json:"quality"`
	References       []string             `json:"references"`
	ProjectID        string               `json:"projectID"`
	Connection       Connection           `json:"connection"`
	Results          []Output             `json:"results,omitempty"`
	AssetIDs         []string             `json:"assetIDs"`
	Diagnostic       GenerationDiagnostic `json:"diagnostic,omitempty"`
}
type GenerationDiagnostic struct {
	Stage             string `json:"stage"`
	StartedAt         string `json:"startedAt"`
	RequestURL        string `json:"requestURL"`
	HTTPStatus        int    `json:"httpStatus"`
	ContentType       string `json:"contentType"`
	ContentLength     int64  `json:"contentLength"`
	TransferEncoding  string `json:"transferEncoding"`
	HTTPProtocol      string `json:"httpProtocol"`
	NetworkRoute      string `json:"networkRoute"`
	UpstreamRequestID string `json:"upstreamRequestID"`
	// UpstreamJobID is the job of an async-jobs connection; with it a restarted
	// app polls the same job instead of submitting again.
	UpstreamJobID string `json:"upstreamJobID,omitempty"`
	ElapsedMS     int64  `json:"elapsedMS"`
	BytesReceived int    `json:"bytesReceived"`
	NetworkError  string `json:"networkError"`
	// Download of a returned result runs after generation, in its own pool.
	DownloadStartedAt string `json:"downloadStartedAt,omitempty"`
	DownloadMS        int64  `json:"downloadMS,omitempty"`
}
type Output struct {
	URL    string `json:"url,omitempty"`
	Base64 string `json:"b64_json,omitempty"`
}
type Asset struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Path      string `json:"path"`
	Thumbnail string `json:"thumbnail"`
	Kind      string `json:"kind"`
	TaskID    string `json:"taskID"`
	ProjectID string `json:"projectID"`
	CreatedAt string `json:"createdAt"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
	Hidden    bool   `json:"hidden"`
}
type Project struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	CreatedAt string `json:"createdAt"`
}
type Template struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Prompt  string `json:"prompt"`
	Size    string `json:"size"`
	Quality string `json:"quality"`
}
type Page struct {
	Items []Task `json:"items"`
	Total int    `json:"total"`
}
type RecentBatch struct {
	ID         string `json:"id"`
	CreatedAt  string `json:"createdAt"`
	Total      int    `json:"total"`
	Completed  int    `json:"completed"`
	Active     int    `json:"active"`
	ImageCount int    `json:"imageCount"`
	Tasks      []Task `json:"tasks"`
}
type AssetPage struct {
	Items []Asset `json:"items"`
	Total int     `json:"total"`
}
type Summary struct {
	Tasks   int    `json:"tasks"`
	Assets  int    `json:"assets"`
	Active  int    `json:"active"`
	DataDir string `json:"dataDir"`
	Version string `json:"version"`
}
type TestResult struct {
	Auth       string   `json:"auth"`
	Models     string   `json:"models"`
	Generation string   `json:"generation"`
	IDs        []string `json:"ids"`
}
