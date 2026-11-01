package admin

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ed25519"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
)

// The signing key proves customer.dat came from this admin tool. Losing it means
// no further packages for existing customers (their apps trust only this key),
// so it can be exported as a password-protected backup.

const backupIters = 600_000

type signingBackup struct {
	Format     int    `json:"format"`
	PublicKey  string `json:"publicKey"`
	Salt       string `json:"salt"`
	Nonce      string `json:"nonce"`
	Data       string `json:"data"`
	Iterations int    `json:"iterations"`
}

// EnsureSigningKey creates the signing key on first run.
func (a *Admin) EnsureSigningKey() (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.d.SigningPublicKey != "" {
		return a.d.SigningPublicKey, nil
	}
	pub, priv, e := ed25519.GenerateKey(nil)
	if e != nil {
		return "", e
	}
	ref, e := a.putSecret(priv)
	if e != nil {
		return "", e
	}
	a.d.SigningPublicKey, a.d.SigningKeyRef = base64.StdEncoding.EncodeToString(pub), ref
	return a.d.SigningPublicKey, a.saveLocked()
}

func (a *Admin) signingKeyLocked() (ed25519.PrivateKey, error) {
	if a.d.SigningKeyRef == "" {
		return nil, errors.New("还没有签名密钥")
	}
	b, e := a.secret(a.d.SigningKeyRef)
	if e != nil || len(b) != ed25519.PrivateKeySize {
		return nil, errors.New("签名密钥不可用，请从备份恢复")
	}
	return ed25519.PrivateKey(b), nil
}

func backupCipher(password string, salt []byte, iters int) (cipher.AEAD, error) {
	key, e := pbkdf2.Key(sha256.New, password, salt, iters, 32)
	if e != nil {
		return nil, e
	}
	block, e := aes.NewCipher(key)
	if e != nil {
		return nil, e
	}
	return cipher.NewGCM(block)
}

// ExportSigningBackup writes the signing key encrypted with a password.
func (a *Admin) ExportSigningBackup(path, password string) error {
	if len([]rune(password)) < 10 {
		return errors.New("备份密码至少 10 个字符")
	}
	a.mu.Lock()
	priv, e := a.signingKeyLocked()
	pub := a.d.SigningPublicKey
	a.mu.Unlock()
	if e != nil {
		return e
	}
	salt := make([]byte, 16)
	rand.Read(salt)
	aead, e := backupCipher(password, salt, backupIters)
	if e != nil {
		return e
	}
	nonce := make([]byte, aead.NonceSize())
	rand.Read(nonce)
	enc := base64.StdEncoding.EncodeToString
	b, _ := json.MarshalIndent(signingBackup{Format: 1, PublicKey: pub, Salt: enc(salt), Nonce: enc(nonce), Data: enc(aead.Seal(nil, nonce, priv, []byte(pub))), Iterations: backupIters}, "", "  ")
	return os.WriteFile(path, b, 0o600)
}

// ImportSigningBackup restores the signing key (e.g. on a new computer). It
// refuses to replace a different key unless replace is set.
func (a *Admin) ImportSigningBackup(path, password string, replace bool) error {
	b, e := os.ReadFile(path)
	if e != nil {
		return e
	}
	var s signingBackup
	if json.Unmarshal(b, &s) != nil || s.Format != 1 {
		return errors.New("不是有效的签名密钥备份")
	}
	dec := base64.StdEncoding.DecodeString
	salt, e1 := dec(s.Salt)
	nonce, e2 := dec(s.Nonce)
	data, e3 := dec(s.Data)
	if e1 != nil || e2 != nil || e3 != nil || s.Iterations < 100_000 {
		return errors.New("不是有效的签名密钥备份")
	}
	aead, e := backupCipher(password, salt, s.Iterations)
	if e != nil || len(nonce) != aead.NonceSize() {
		return errors.New("不是有效的签名密钥备份")
	}
	priv, e := aead.Open(nil, nonce, data, []byte(s.PublicKey))
	if e != nil || len(priv) != ed25519.PrivateKeySize {
		return errors.New("备份密码不正确")
	}
	if base64.StdEncoding.EncodeToString(ed25519.PrivateKey(priv).Public().(ed25519.PublicKey)) != s.PublicKey {
		return errors.New("备份内容不一致")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.d.SigningPublicKey != "" && a.d.SigningPublicKey != s.PublicKey && !replace {
		return errors.New("当前已有另一把签名密钥；替换后已交付客户将无法用新包升级，需要明确确认")
	}
	ref, e := a.putSecret(priv)
	if e != nil {
		return e
	}
	a.d.SigningPublicKey, a.d.SigningKeyRef = s.PublicKey, ref
	return a.saveLocked()
}
