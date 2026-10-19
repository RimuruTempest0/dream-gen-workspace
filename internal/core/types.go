package core

type Connection struct {
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
	TestRequestID   string `json:"testRequestID,omitempty"`
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
}
type Task struct {
	AgentRunID string `json:"agentRunID,omitempty"`
	PlanItemID string `json:"planItemID,omitempty"`
	// RefinedFrom is the task whose image this attempt adjusts; RefineInstruction is the user's request.
	RefinedFrom       string `json:"refinedFrom,omitempty"`
	RefineInstruction string `json:"refineInstruction,omitempty"`
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
	ElapsedMS         int64  `json:"elapsedMS"`
	BytesReceived     int    `json:"bytesReceived"`
	NetworkError      string `json:"networkError"`
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
