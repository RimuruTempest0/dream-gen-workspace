package core

type LLMCapabilities struct {
	Model     string   `json:"model"`
	Text      bool     `json:"text"`
	Vision    bool     `json:"vision"`
	Tools     bool     `json:"tools"`
	Streaming bool     `json:"streaming"`
	CheckedAt string   `json:"checkedAt"`
	Details   []string `json:"details"`
}
type AgentInput struct {
	ProductURL        string   `json:"productURL"`
	Instruction       string   `json:"instruction"`
	ProductText       string   `json:"productText"`
	References        []string `json:"references"`
	ProjectID         string   `json:"projectID"`
	TargetCount       int      `json:"targetCount"`
	Ratio             string   `json:"ratio"`
	Size              string   `json:"size"`
	Quality           string   `json:"quality"`
	LLMConnectionID   string   `json:"llmConnectionID"`
	ImageConnectionID string   `json:"imageConnectionID"`
	ImageModel        string   `json:"imageModel"`
	SkillIDs          []string `json:"skillIDs"`
}
type ProductInfo struct {
	ImageSources map[string]string `json:"imageSources"`
	SourceURL    string            `json:"sourceURL"`
	Title        string            `json:"title"`
	Description  string            `json:"description"`
	Facts        string            `json:"facts"`
	Error        string            `json:"error"`
	ReferenceIDs []string          `json:"referenceIDs"`
}
type AgentQuestion struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Required bool   `json:"required"`
	Answer   string `json:"answer"`
}
type SuiteItem struct {
	ID         string   `json:"id"`
	Purpose    string   `json:"purpose"`
	Scene      string   `json:"scene"`
	Prompt     string   `json:"prompt"`
	References []string `json:"references"`
	Size       string   `json:"size"`
	Quality    string   `json:"quality"`
}
type SuitePlan struct {
	Revision  int         `json:"revision"`
	CreatedAt string      `json:"createdAt"`
	Locked    []string    `json:"locked"`
	Editable  []string    `json:"editable"`
	Notes     string      `json:"notes"`
	Items     []SuiteItem `json:"items"`
}
type AgentEvent struct {
	At   string `json:"at"`
	Text string `json:"text"`
}
type SkillAttachment struct {
	Name string `json:"name"`
	Path string `json:"path"`
}
type SkillRecord struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Content     string            `json:"content"`
	Version     int               `json:"version"`
	Enabled     bool              `json:"enabled"`
	Source      string            `json:"source"`
	Attachments []SkillAttachment `json:"attachments"`
}
type AgentRun struct {
	AutoGeneration       bool            `json:"autoGeneration"`
	AuthorizedCount      int             `json:"authorizedCount"`
	ImageConnection      Connection      `json:"imageConnection"`
	SubmissionID         string          `json:"submissionID"`
	AttemptStartRevision int             `json:"attemptStartRevision"`
	AnalysisSucceeded    bool            `json:"analysisSucceeded"`
	CancelRequested      bool            `json:"cancelRequested"`
	AnalysisStartedAt    string          `json:"analysisStartedAt"`
	Instructions         []string        `json:"instructions"`
	ID                   string          `json:"id"`
	CreatedAt            string          `json:"createdAt"`
	UpdatedAt            string          `json:"updatedAt"`
	Status               string          `json:"status"`
	Stage                string          `json:"stage"`
	Error                string          `json:"error"`
	Input                AgentInput      `json:"input"`
	LLMConnection        Connection      `json:"llmConnection"`
	Skills               []SkillRecord   `json:"skills"`
	Product              ProductInfo     `json:"product"`
	Questions            []AgentQuestion `json:"questions"`
	Plans                []SuitePlan     `json:"plans"`
	Events               []AgentEvent    `json:"events"`
	BatchID              string          `json:"batchID"`
	SubmittedRevision    int             `json:"submittedRevision"`
}
type AgentPage struct {
	Items []AgentRun `json:"items"`
	Total int        `json:"total"`
}
type AgentContinuation struct {
	RunID       string            `json:"runID"`
	Instruction string            `json:"instruction"`
	ProductText string            `json:"productText"`
	References  []string          `json:"references"`
	Answers     map[string]string `json:"answers"`
}
type AgentRuntimeInfo struct {
	Available bool   `json:"available"`
	Version   string `json:"version"`
	Message   string `json:"message"`
}
