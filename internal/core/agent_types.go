package core

import (
	"encoding/json"
	"strings"
)

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
	// AutoMode runs without asking the user: missing facts are decided by the Agent.
	AutoMode bool `json:"autoMode,omitempty"`
}

// ProductInfo.Error means the page itself could not be read; ImageNote only says
// some candidate images could not be saved while the page was read.
type ProductInfo struct {
	ImageSources map[string]string `json:"imageSources"`
	SourceURL    string            `json:"sourceURL"`
	Title        string            `json:"title"`
	Description  string            `json:"description"`
	Facts        string            `json:"facts"`
	Error        string            `json:"error"`
	ImageNote    string            `json:"imageNote,omitempty"`
	ReferenceIDs []string          `json:"referenceIDs"`
}

// UnmarshalJSON moves the partial-image notes that versions before 0.6.7 stored
// in Error, so those runs no longer report an unreadable page, and flags saved
// block pages.
func (p *ProductInfo) UnmarshalJSON(b []byte) error {
	type plain ProductInfo
	if e := json.Unmarshal(b, (*plain)(p)); e != nil {
		return e
	}
	if strings.HasPrefix(p.Error, "部分商品图片") {
		p.ImageNote, p.Error = p.Error, ""
	}
	// Before 0.6.14 Amazon's "Continue shopping" page was saved as the product;
	// marking it unread makes continuing read the page again.
	if p.Error == "" && challengeInfo(*p) {
		p.Title, p.Description, p.Error = "", "", "商品页返回人机验证页，继续制作时会重新读取；也可上传图片或粘贴商品描述"
	}
	return nil
}

type AgentQuestion struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Required bool   `json:"required"`
	Answer   string `json:"answer"`
	// AnsweredBy is "agent" when an automatic run answered the question itself;
	// Basis says where the answer came from (see answerBases) and Reason why.
	AnsweredBy string `json:"answeredBy,omitempty"`
	Basis      string `json:"basis,omitempty"`
	Reason     string `json:"reason,omitempty"`
}

// AnswerHabit is one of the user's recent manual-mode answers, given to automatic
// runs so the Agent learns how the user decides.
type AnswerHabit struct {
	Question string `json:"question"`
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
	// Sample marks a sample of the suite: since 0.7.9 one per design area
	// (SampleArea, e.g. 胸前 for the main design's master, 袖口 for a cuff close-up).
	// Items wait for the samples they use and get them as their first references:
	// UseSamples names the areas; UseSample alone (plans before 0.7.9) means all.
	Sample     bool     `json:"sample,omitempty"`
	SampleArea string   `json:"sampleArea,omitempty"`
	UseSample  bool     `json:"useSample,omitempty"`
	UseSamples []string `json:"useSamples,omitempty"`
	// NoProduct marks an image without the product (a thread-colour card, fabric
	// swatches); it is sent without the locked product spec.
	NoProduct bool `json:"noProduct,omitempty"`
}
type SuitePlan struct {
	Revision  int         `json:"revision"`
	CreatedAt string      `json:"createdAt"`
	Locked    []string    `json:"locked"`
	Editable  []string    `json:"editable"`
	Notes     string      `json:"notes"`
	Items     []SuiteItem `json:"items"`
	// CuffEmbroidery says whether the product has cuff embroidery; nil on plans
	// saved before 0.5.2. False drops the P004 cuff clauses from image prompts.
	CuffEmbroidery *bool `json:"cuffEmbroidery,omitempty"`
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
	AutoGeneration       bool       `json:"autoGeneration"`
	AuthorizedCount      int        `json:"authorizedCount"`
	ImageConnection      Connection `json:"imageConnection"`
	SubmissionID         string     `json:"submissionID"`
	AttemptStartRevision int        `json:"attemptStartRevision"`
	AnalysisSucceeded    bool       `json:"analysisSucceeded"`
	CancelRequested      bool       `json:"cancelRequested"`
	AnalysisStartedAt    string     `json:"analysisStartedAt"`
	Instructions         []string   `json:"instructions"`
	ID                   string     `json:"id"`
	CreatedAt            string     `json:"createdAt"`
	UpdatedAt            string     `json:"updatedAt"`
	Status               string     `json:"status"`
	Stage                string     `json:"stage"`
	Error                string     `json:"error"`
	// LLMReconnects counts LLM streams cut during this analysis; Claude Code
	// retries them itself, so they are a note, not an error, unless it fails.
	LLMReconnects int `json:"llmReconnects,omitempty"`
	// AutoContinues counts how often an automatic run was continued by itself.
	AutoContinues int `json:"autoContinues,omitempty"`
	// AnswerHabits are the user's recent manual answers, taken when an automatic run starts.
	AnswerHabits []AnswerHabit `json:"answerHabits,omitempty"`
	// Crops are reference images the Agent cut from this run's references.
	Crops             []string `json:"crops,omitempty"`
	LLMReconnectError string   `json:"llmReconnectError,omitempty"`
	// WaitedMS is the time the run stood waiting for the user (answers, plan
	// review, a decision to continue); the elapsed timer leaves it out.
	WaitedMS int64 `json:"waitedMS,omitempty"`
	// Progress is the current step of the analysis, shown to the customer.
	Progress *RunProgress `json:"progress,omitempty"`
	// Activity is the feed of what Claude Code and the LLM did. Never sent to the LLM.
	Activity []ActivityEntry `json:"activity,omitempty"`
	// StopReason says why the app ended this attempt itself.
	StopReason        string          `json:"stopReason,omitempty"`
	Input             AgentInput      `json:"input"`
	LLMConnection     Connection      `json:"llmConnection"`
	Skills            []SkillRecord   `json:"skills"`
	Product           ProductInfo     `json:"product"`
	Questions         []AgentQuestion `json:"questions"`
	Plans             []SuitePlan     `json:"plans"`
	Events            []AgentEvent    `json:"events"`
	BatchID           string          `json:"batchID"`
	SubmittedRevision int             `json:"submittedRevision"`
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
