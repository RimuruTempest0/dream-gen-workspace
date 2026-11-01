// Package admin is the ImageWorkbench admin tool's logic: a ledger of customers,
// software IDs and activation codes, a pool of upstream keys, and the customer
// packages built from them. It runs only on the provider's own computer. Keys,
// activation codes and the signing key are stored with Windows DPAPI; the ledger
// file itself holds no secret.
package admin

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"imageworkbench/internal/core"
)

type Upstream struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Kind            string `json:"kind"` // image or llm
	BaseURL         string `json:"baseURL"`
	Model           string `json:"model"`
	Mode            string `json:"mode,omitempty"`     // llm: claude or compatible
	AuthMode        string `json:"authMode,omitempty"` // llm: api-key or bearer
	MaxOutputTokens int    `json:"maxOutputTokens,omitempty"`
	// PerKeyQuota: the upstream can cap a single key (e.g. Sub2API: 幻灵, 1 Route).
	// Without it a key cannot be limited on its own (e.g. DeepSeek official): every
	// key ships in the package before payment, so only its quota guards it.
	PerKeyQuota bool `json:"perKeyQuota"`
	// ConsoleURL is the upstream's web backend, opened to set quotas by hand.
	ConsoleURL string `json:"consoleURL,omitempty"`
	// UsageURL is the free self-query of a key's quota (Sub2API /v1/usage); the
	// customer app shows the remaining trial quota from it.
	UsageURL string `json:"usageURL,omitempty"`
}

// PoolKey is one upstream key. Status: available, assigned or disabled. A key
// that went out in a package is disabled when it comes off its customer and is
// only handed out again if the provider re-enables it.
type PoolKey struct {
	ID         string `json:"id"`
	Hash       string `json:"hash"` // sha256 of the key, for de-duplication
	Tail       string `json:"tail"` // last 4 characters, to tell keys apart
	SecretRef  string `json:"secretRef"`
	UpstreamID string `json:"upstreamID"`
	Kind       string `json:"kind"`
	Use        string `json:"use"` // primary or backup
	QuotaNote  string `json:"quotaNote,omitempty"`
	Status     string `json:"status"`
	CustomerID string `json:"customerID,omitempty"`
	Slot       string `json:"slot,omitempty"`
	Delivered  bool   `json:"delivered,omitempty"`
	// DeliveredBefore marks a re-enabled key that went out in an earlier package.
	DeliveredBefore bool   `json:"deliveredBefore,omitempty"`
	Verified        bool   `json:"verified,omitempty"`
	VerifiedAt      string `json:"verifiedAt,omitempty"`
	ImportedAt      string `json:"importedAt"`
	DisabledFor     string `json:"disabledFor,omitempty"`
}

type Delivery struct {
	Version       string `json:"version"`
	At            string `json:"at"`
	Zip           string `json:"zip"`
	SHA256        string `json:"sha256"`
	VaultRevision int    `json:"vaultRevision"`
}

type Event struct {
	At   string `json:"at"`
	Kind string `json:"kind"` // created, assigned, rekeyed, delivered, renewed, note
	Text string `json:"text"`
}

type Customer struct {
	ID            string            `json:"id"`
	Name          string            `json:"name"`
	SoftwareID    string            `json:"softwareID"`
	CodeRef       string            `json:"codeRef"`
	CreatedAt     string            `json:"createdAt"`
	TrialImages   int               `json:"trialImages"`
	PlanNote      string            `json:"planNote,omitempty"`  // 套餐与有效期
	QuotaNote     string            `json:"quotaNote,omitempty"` // 额度备注
	Slots         map[string]string `json:"slots"`               // slot → key ID
	ImageBackups  []string          `json:"imageBackups"`        // image slots after image-1, in order
	VaultRevision int               `json:"vaultRevision"`       // of the last package
	Changed       bool              `json:"changed"`             // keys changed since the last package
	// Paid: the customer paid and the keys' upstream quotas were raised; the
	// activation code is meant to be sent only then.
	Paid       bool       `json:"paid"`
	PaidAt     string     `json:"paidAt,omitempty"`
	Deliveries []Delivery `json:"deliveries"`
	Events     []Event    `json:"events"`
}

type ledgerData struct {
	Version          int        `json:"version"`
	SigningPublicKey string     `json:"signingPublicKey,omitempty"`
	SigningKeyRef    string     `json:"signingKeyRef,omitempty"`
	RepoRoot         string     `json:"repoRoot,omitempty"`
	Upstreams        []Upstream `json:"upstreams"`
	Keys             []PoolKey  `json:"keys"`
	Customers        []Customer `json:"customers"`
}

// Slots of one customer package: what the customer app calls each line.
var (
	ImageSlots   = []string{"image-1", "image-2", "image-3", "image-4", "image-5", "image-6"}
	LLMSlots     = []string{"llm-1", "llm-2"}
	AllSlots     = slices.Concat(ImageSlots, LLMSlots)
	defaultOrder = []string{"image-2", "image-3", "image-4", "image-5", "image-6"}
	// RequiredSlots must have a key before a package can be built, plus at least
	// one backup image line (any of defaultOrder); empty lines are left out.
	RequiredSlots = []string{"image-1", "llm-1"}
)

// SlotNeeds gives the kind of key a slot takes and its role (primary/backup).
func SlotNeeds(slot string) (kind, use string) {
	switch {
	case slot == "llm-1":
		return "llm", "primary"
	case slot == "llm-2":
		return "llm", "backup"
	case slot == "image-1" || slot == "image-2" || slot == "image-3":
		return "image", "primary"
	}
	return "image", "backup"
}

// SlotName is the line name customers see.
func SlotName(slot string) string {
	switch slot {
	case "llm-1":
		return "LLM 线路 1"
	case "llm-2":
		return "LLM 线路 2"
	}
	return "图片线路 " + strings.TrimPrefix(slot, "image-")
}

// Admin is the tool's state, kept in <root>/ledger.json plus DPAPI secret files.
type Admin struct {
	mu   sync.Mutex
	root string
	d    ledgerData
	// protect/unprotect are core.Protect/Unprotect (DPAPI); tests may replace them.
	protect   func([]byte) ([]byte, error)
	unprotect func([]byte) ([]byte, error)
}

func Open(root string) (*Admin, error) {
	if e := os.MkdirAll(filepath.Join(root, "secrets"), 0o700); e != nil {
		return nil, e
	}
	a := &Admin{root: root, protect: core.Protect, unprotect: core.Unprotect, d: ledgerData{Version: 1}}
	b, e := os.ReadFile(a.ledgerPath())
	if e == nil {
		if e = json.Unmarshal(b, &a.d); e != nil {
			return nil, fmt.Errorf("台账文件已损坏：%w", e)
		}
	} else if !errors.Is(e, os.ErrNotExist) {
		return nil, e
	}
	return a, nil
}

func (a *Admin) Root() string       { return a.root }
func (a *Admin) ledgerPath() string { return filepath.Join(a.root, "ledger.json") }

func newID() string {
	b := make([]byte, 12)
	rand.Read(b)
	return hex.EncodeToString(b)
}
func now() string { return time.Now().UTC().Format(time.RFC3339) }

// saveLocked writes the ledger atomically; a crash never leaves half a file.
func (a *Admin) saveLocked() error {
	b, e := json.MarshalIndent(a.d, "", "  ")
	if e != nil {
		return e
	}
	tmp, e := os.CreateTemp(a.root, ".ledger-*")
	if e != nil {
		return e
	}
	defer os.Remove(tmp.Name())
	if _, e = tmp.Write(b); e == nil {
		e = tmp.Sync()
	}
	if c := tmp.Close(); e == nil {
		e = c
	}
	if e != nil {
		return e
	}
	return os.Rename(tmp.Name(), a.ledgerPath())
}

func (a *Admin) putSecret(value []byte) (string, error) {
	sealed, e := a.protect(value)
	if e != nil {
		return "", e
	}
	ref := newID()
	return ref, os.WriteFile(filepath.Join(a.root, "secrets", ref+".bin"), sealed, 0o600)
}

func (a *Admin) removeSecret(ref string) {
	if ref != "" && !strings.ContainsAny(ref, `/\.`) {
		os.Remove(filepath.Join(a.root, "secrets", ref+".bin"))
	}
}

func (a *Admin) secret(ref string) ([]byte, error) {
	if ref == "" || strings.ContainsAny(ref, `/\.`) {
		return nil, errors.New("密钥记录不存在")
	}
	sealed, e := os.ReadFile(filepath.Join(a.root, "secrets", ref+".bin"))
	if e != nil {
		return nil, errors.New("密钥文件不可用")
	}
	plain, e := a.unprotect(sealed)
	if e != nil {
		return nil, errors.New("密钥无法在当前 Windows 用户下解密")
	}
	return plain, nil
}

func keyHash(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

func (a *Admin) customerLocked(id string) (*Customer, error) {
	for i := range a.d.Customers {
		if a.d.Customers[i].ID == id {
			return &a.d.Customers[i], nil
		}
	}
	return nil, errors.New("客户不存在")
}

func (a *Admin) keyLocked(id string) (*PoolKey, error) {
	for i := range a.d.Keys {
		if a.d.Keys[i].ID == id {
			return &a.d.Keys[i], nil
		}
	}
	return nil, errors.New("Key 不存在")
}

func (a *Admin) upstreamLocked(id string) (*Upstream, error) {
	for i := range a.d.Upstreams {
		if a.d.Upstreams[i].ID == id {
			return &a.d.Upstreams[i], nil
		}
	}
	return nil, errors.New("上游不存在")
}

func (c *Customer) log(kind, text string) {
	c.Events = append(c.Events, Event{At: now(), Kind: kind, Text: text})
}
