// Package license defines the customer file shipped next to ImageWorkbench.exe
// (customer.dat): a software identity, the customer's lines (keys obfuscated), and
// a check value for the activation code. The app reads it; the admin tool writes
// it. Only the Go standard library is used.
//
// Trial and normal use run on the same keys. The hard limit is the quota each key
// has at the upstream: low until the customer pays, then raised by the provider.
// The app's trial counter and the activation code only steer the software.
package license

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ed25519"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

const (
	FileName      = "customer.dat"
	Format        = 2
	codeIters     = 200_000
	codeGroups    = 5
	codeGroupSize = 5
)

// Crockford base32: no I, L, O, U, so codes read and type unambiguously.
const alphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// Connection is one upstream line. Slot names it for the app: "image-1" … "image-6"
// and "llm-1", "llm-2"; image-1 and llm-1 are the lines used during the trial.
type Connection struct {
	Slot            string `json:"slot"`
	Kind            string `json:"kind"` // image or llm
	Name            string `json:"name"`
	BaseURL         string `json:"baseURL"`
	Model           string `json:"model"`
	Mode            string `json:"mode,omitempty"`
	AuthMode        string `json:"authMode,omitempty"`
	MaxOutputTokens int    `json:"maxOutputTokens,omitempty"`
	// UsageURL is the upstream's free self-query of a key's quota (Sub2API
	// /v1/usage), shown to customers as the remaining trial quota.
	UsageURL string `json:"usageURL,omitempty"`
	// Verified marks an LLM line the admin checked for text, image input, tools
	// and streaming, so the customer never has to run that check.
	Verified bool   `json:"verified,omitempty"`
	Key      string `json:"key,omitempty"`    // never in a signed file
	KeyObf   string `json:"keyObf,omitempty"` // obfuscated: the quota is the guard
}

// CodeCheck lets the app verify the activation code offline. The code has 125
// random bits, so it cannot be guessed from this value.
type CodeCheck struct {
	Salt       string `json:"salt"`
	Hash       string `json:"hash"`
	Iterations int    `json:"iterations"`
}

type Profile struct {
	Format        int          `json:"format"`
	SoftwareID    string       `json:"softwareID"`
	Customer      string       `json:"customer"`
	IssuedAt      string       `json:"issuedAt"`
	TrialImages   int          `json:"trialImages"`
	Connections   []Connection `json:"connections"`
	ImageBackups  []string     `json:"imageBackups"` // image slots after image-1, in order
	LinesRevision int          `json:"linesRevision"`
	CodeCheck     CodeCheck    `json:"codeCheck"`
}

type file struct {
	Format    int    `json:"format"`
	Payload   string `json:"payload"`
	Signature string `json:"signature"`
}

var (
	ErrSignature = errors.New("软件授权文件无效或已被修改")
	ErrCode      = errors.New("激活码与本软件不匹配，请核对后重新输入")
)

func randomChars(n int) (string, error) {
	b := make([]byte, n)
	if _, e := rand.Read(b); e != nil {
		return "", e
	}
	out := make([]byte, n)
	for i, v := range b {
		out[i] = alphabet[v&31] // 256 is a multiple of 32: no bias
	}
	return string(out), nil
}

// NewSoftwareID returns an identifier like IW-7K3Q-M9TA-2XRB.
func NewSoftwareID() (string, error) {
	s, e := randomChars(12)
	if e != nil {
		return "", e
	}
	return "IW-" + s[:4] + "-" + s[4:8] + "-" + s[8:], nil
}

// NewCode returns a 25-character activation code (125 random bits) in 5 groups.
func NewCode() (string, error) {
	s, e := randomChars(codeGroups * codeGroupSize)
	if e != nil {
		return "", e
	}
	groups := make([]string, codeGroups)
	for i := range groups {
		groups[i] = s[i*codeGroupSize : (i+1)*codeGroupSize]
	}
	return strings.Join(groups, "-"), nil
}

// NormalizeCode accepts what people type: any case, spaces or dashes, and the
// look-alikes O/I/L for 0/1/1.
func NormalizeCode(code string) (string, error) {
	var b strings.Builder
	for _, r := range strings.ToUpper(code) {
		switch {
		case r == '-' || r == ' ' || r == '\t' || r == '　':
			continue
		case r == 'O':
			r = '0'
		case r == 'I' || r == 'L':
			r = '1'
		}
		if !strings.ContainsRune(alphabet, r) {
			return "", ErrCode
		}
		b.WriteRune(r)
	}
	if b.Len() != codeGroups*codeGroupSize {
		return "", errors.New("激活码应为 25 位字母和数字（可带短横线）")
	}
	return b.String(), nil
}

func codeHash(norm, softwareID string, salt []byte, iters int) ([]byte, error) {
	return pbkdf2.Key(sha256.New, norm+"|"+softwareID, salt, iters, 32)
}

// NewCodeCheck derives the check value stored in customer.dat for a code.
func NewCodeCheck(code, softwareID string) (CodeCheck, error) {
	norm, e := NormalizeCode(code)
	if e != nil {
		return CodeCheck{}, e
	}
	salt := make([]byte, 16)
	if _, e = rand.Read(salt); e != nil {
		return CodeCheck{}, e
	}
	h, e := codeHash(norm, softwareID, salt, codeIters)
	if e != nil {
		return CodeCheck{}, e
	}
	enc := base64.StdEncoding.EncodeToString
	return CodeCheck{Salt: enc(salt), Hash: enc(h), Iterations: codeIters}, nil
}

// CheckCode reports whether code is this software's activation code.
func CheckCode(cc CodeCheck, code, softwareID string) error {
	norm, e := NormalizeCode(code)
	if e != nil {
		return e
	}
	salt, e1 := base64.StdEncoding.DecodeString(cc.Salt)
	want, e2 := base64.StdEncoding.DecodeString(cc.Hash)
	if e1 != nil || e2 != nil || cc.Iterations < 10_000 {
		return ErrSignature
	}
	got, e := codeHash(norm, softwareID, salt, cc.Iterations)
	if e != nil || subtle.ConstantTimeCompare(got, want) != 1 {
		return ErrCode
	}
	return nil
}

func gcmFor(key []byte) (cipher.AEAD, error) {
	block, e := aes.NewCipher(key)
	if e != nil {
		return nil, e
	}
	return cipher.NewGCM(block)
}

// Keys travel in the package and are only obfuscated: the hard limit is the
// upstream quota on each key, not this encryption.
func keyAEAD(softwareID string) (cipher.AEAD, error) {
	sum := sha256.Sum256([]byte("imageworkbench-lines-v2|" + softwareID))
	return gcmFor(sum[:])
}

func ObfuscateKey(key, softwareID string) (string, error) {
	aead, e := keyAEAD(softwareID)
	if e != nil {
		return "", e
	}
	nonce := make([]byte, aead.NonceSize())
	if _, e = rand.Read(nonce); e != nil {
		return "", e
	}
	return base64.StdEncoding.EncodeToString(append(nonce, aead.Seal(nil, nonce, []byte(key), nil)...)), nil
}

func RevealKey(obf, softwareID string) (string, error) {
	raw, e := base64.StdEncoding.DecodeString(obf)
	aead, e2 := keyAEAD(softwareID)
	if e != nil || e2 != nil || len(raw) <= aead.NonceSize() {
		return "", ErrSignature
	}
	plain, e := aead.Open(nil, raw[:aead.NonceSize()], raw[aead.NonceSize():], nil)
	if e != nil {
		return "", ErrSignature
	}
	return string(plain), nil
}

// Sign produces the customer.dat bytes. Keys must already be obfuscated.
func Sign(p Profile, priv ed25519.PrivateKey) ([]byte, error) {
	p.Format = Format
	for _, c := range p.Connections {
		if c.Key != "" {
			return nil, errors.New("customer.dat 不能包含 Key 明文")
		}
	}
	payload, e := json.Marshal(p)
	if e != nil {
		return nil, e
	}
	return json.MarshalIndent(file{Format: Format, Payload: base64.StdEncoding.EncodeToString(payload), Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(priv, payload))}, "", "  ")
}

// Verify checks the signature against any trusted public key (base64) and returns
// the profile.
func Verify(data []byte, publicKeys []string) (Profile, error) {
	var f file
	if json.Unmarshal(data, &f) != nil || f.Format != Format {
		return Profile{}, ErrSignature
	}
	payload, e1 := base64.StdEncoding.DecodeString(f.Payload)
	sig, e2 := base64.StdEncoding.DecodeString(f.Signature)
	if e1 != nil || e2 != nil {
		return Profile{}, ErrSignature
	}
	trusted := false
	for _, k := range publicKeys {
		pub, e := base64.StdEncoding.DecodeString(strings.TrimSpace(k))
		if e == nil && len(pub) == ed25519.PublicKeySize && ed25519.Verify(pub, payload, sig) {
			trusted = true
			break
		}
	}
	if !trusted {
		return Profile{}, ErrSignature
	}
	var p Profile
	if e := json.Unmarshal(payload, &p); e != nil || p.Format != Format || p.SoftwareID == "" {
		return Profile{}, ErrSignature
	}
	if p.TrialImages <= 0 {
		p.TrialImages = 20
	}
	return p, nil
}

// CheckLines reports problems in the lines before they are signed. Keys are
// checked in plain form (Key) or obfuscated (KeyObf).
func CheckLines(conns []Connection, backups []string) error {
	seen := map[string]Connection{}
	for _, c := range conns {
		if c.Slot == "" || c.BaseURL == "" || c.Model == "" || (c.Key == "" && c.KeyObf == "") || (c.Kind != "image" && c.Kind != "llm") {
			return fmt.Errorf("线路 %q 缺少地址、模型或 Key", c.Slot)
		}
		if _, dup := seen[c.Slot]; dup {
			return fmt.Errorf("线路 %q 重复", c.Slot)
		}
		seen[c.Slot] = c
	}
	if seen["image-1"].Kind != "image" || seen["llm-1"].Kind != "llm" {
		return errors.New("必须包含图片线路 image-1 和 LLM 线路 llm-1")
	}
	for _, s := range backups {
		if seen[s].Kind != "image" || s == "image-1" {
			return fmt.Errorf("备用图片线路 %q 无效", s)
		}
	}
	return nil
}
