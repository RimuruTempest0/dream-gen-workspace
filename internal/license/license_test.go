package license

import (
	"crypto/ed25519"
	"encoding/base64"
	"strings"
	"testing"
)

func lines() []Connection {
	return []Connection{
		{Slot: "image-1", Kind: "image", Name: "图片线路 1", BaseURL: "https://a.test/v1", Model: "gpt-image-2", Key: "sk-image-secret"},
		{Slot: "image-2", Kind: "image", Name: "图片线路 2", BaseURL: "https://b.test/v1", Model: "gpt-image-2", Key: "sk-image-2"},
		{Slot: "llm-1", Kind: "llm", Name: "LLM 线路 1", BaseURL: "https://l.test/v1", Model: "m", Key: "sk-llm-secret", Verified: true},
	}
}

func TestCodesAndIDs(t *testing.T) {
	code, _ := NewCode()
	if len(code) != 29 || strings.Count(code, "-") != 4 {
		t.Fatal(code)
	}
	norm, e := NormalizeCode(" " + strings.ToLower(strings.ReplaceAll(code, "-", " ")) + " ")
	if e != nil || norm != strings.ReplaceAll(code, "-", "") {
		t.Fatal(norm, e)
	}
	if n, _ := NormalizeCode("OOOOO-IIIII-LLLLL-00000-11111"); n != "0000011111111110000011111" {
		t.Fatal("look-alikes", n)
	}
	for _, bad := range []string{"ABC", "UUUUU-UUUUU-UUUUU-UUUUU-UUUUU", "ABCDE-FGHJK-MNPQR-STVWX-YZ01"} {
		if _, e := NormalizeCode(bad); e == nil {
			t.Fatal("accepted", bad)
		}
	}
	id, _ := NewSoftwareID()
	other, _ := NewSoftwareID()
	if !strings.HasPrefix(id, "IW-") || len(id) != 17 || id == other {
		t.Fatal(id, other)
	}
}

func TestCodeCheckAcceptsOnlyThisSoftwaresCode(t *testing.T) {
	code, _ := NewCode()
	other, _ := NewCode()
	cc, e := NewCodeCheck(code, "IW-AAAA-BBBB-CCCC")
	if e != nil || strings.Contains(cc.Hash, code) {
		t.Fatal(cc, e)
	}
	if e = CheckCode(cc, strings.ToLower(code), "IW-AAAA-BBBB-CCCC"); e != nil {
		t.Fatal(e)
	}
	if CheckCode(cc, other, "IW-AAAA-BBBB-CCCC") != ErrCode {
		t.Fatal("wrong code accepted")
	}
	if CheckCode(cc, code, "IW-AAAA-BBBB-DDDD") != ErrCode {
		t.Fatal("code accepted for another software ID")
	}
}

func TestSignedFileRejectsTamperingAndPlainKeys(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(nil)
	trusted := []string{base64.StdEncoding.EncodeToString(pub)}
	id := "IW-AAAA-BBBB-CCCC"
	conns := lines()
	if _, e := Sign(Profile{SoftwareID: id, Connections: conns}, priv); e == nil {
		t.Fatal("plain keys signed")
	}
	for i := range conns {
		conns[i].KeyObf, _ = ObfuscateKey(conns[i].Key, id)
		conns[i].Key = ""
	}
	data, e := Sign(Profile{SoftwareID: id, Customer: "客户 A", Connections: conns, ImageBackups: []string{"image-2"}}, priv)
	if e != nil || strings.Contains(string(data), "sk-") {
		t.Fatal("keys in clear text", e)
	}
	p, e := Verify(data, trusted)
	if e != nil || p.SoftwareID != id || p.TrialImages != 20 || p.Format != 2 {
		t.Fatal(p, e)
	}
	if k, _ := RevealKey(p.Connections[0].KeyObf, id); k != "sk-image-secret" {
		t.Fatal(k)
	}
	if _, e = RevealKey(p.Connections[0].KeyObf, "IW-ZZZZ-ZZZZ-ZZZZ"); e == nil {
		t.Fatal("key revealed with another ID")
	}
	tampered := strings.Replace(string(data), `"payload": "`, `"payload": "e`, 1)
	if _, e = Verify([]byte(tampered), trusted); e != ErrSignature {
		t.Fatal("tampered file accepted")
	}
	otherPub, _, _ := ed25519.GenerateKey(nil)
	if _, e = Verify(data, []string{base64.StdEncoding.EncodeToString(otherPub)}); e != ErrSignature {
		t.Fatal("untrusted signer accepted")
	}
}

func TestCheckLines(t *testing.T) {
	if e := CheckLines(lines(), []string{"image-2"}); e != nil {
		t.Fatal(e)
	}
	if CheckLines(lines(), []string{"llm-1"}) == nil {
		t.Fatal("LLM line accepted as image backup")
	}
	c := lines()
	c[0].Key = ""
	if CheckLines(c, nil) == nil {
		t.Fatal("missing key accepted")
	}
}

func TestExpiryNeedsFormat3AndOldFilesStillOpen(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(nil)
	trusted := []string{base64.StdEncoding.EncodeToString(pub)}
	id := "IW-AAAA-BBBB-CCCC"
	conns := lines()
	for i := range conns {
		conns[i].KeyObf, _ = ObfuscateKey(conns[i].Key, id)
		conns[i].Key = ""
	}
	// No expiry: still written as format 2, so older apps open it.
	plain, _ := Sign(Profile{SoftwareID: id, Connections: conns}, priv)
	if !strings.Contains(string(plain), `"format": 2`) {
		t.Fatal("file without new fields not in format 2")
	}
	if p, e := Verify(plain, trusted); e != nil || p.ExpiresAt != "" {
		t.Fatal(p, e)
	}
	data, e := Sign(Profile{SoftwareID: id, Connections: conns, ExpiresAt: "2027-01-31T16:00:00+08:00"}, priv)
	if e != nil || !strings.Contains(string(data), `"format": 3`) {
		t.Fatal("expiry not in format 3", e)
	}
	p, e := Verify(data, trusted)
	if e != nil || p.Format != 3 || p.ExpiresAt != "2027-01-31T08:00:00Z" {
		t.Fatal(p, e)
	}
	if _, e = Sign(Profile{SoftwareID: id, Connections: conns, ExpiresAt: "next month"}, priv); e == nil {
		t.Fatal("bad expiry signed")
	}
	// The outer format must match the signed one: a format 3 payload relabelled as 2 is refused.
	relabelled := strings.Replace(string(data), `"format": 3`, `"format": 2`, 1)
	if _, e = Verify([]byte(relabelled), trusted); e != ErrSignature {
		t.Fatal("relabelled file accepted")
	}
	future := strings.Replace(string(data), `"format": 3`, `"format": 4`, 1)
	if _, e = Verify([]byte(future), trusted); e != ErrSignature {
		t.Fatal("unknown format accepted")
	}
}

func TestAsyncProtocolNeedsFormat3(t *testing.T) {
	_, priv, _ := ed25519.GenerateKey(nil)
	id := "IW-AAAA-BBBB-CCCC"
	conns := lines()
	for i := range conns {
		conns[i].KeyObf, _ = ObfuscateKey(conns[i].Key, id)
		conns[i].Key = ""
	}
	conns[0].Protocol = "openai-images"
	if data, _ := Sign(Profile{SoftwareID: id, Connections: conns}, priv); !strings.Contains(string(data), `"format": 2`) {
		t.Fatal("default protocol raised the format")
	}
	conns[0].Protocol = "async-jobs"
	if data, _ := Sign(Profile{SoftwareID: id, Connections: conns}, priv); !strings.Contains(string(data), `"format": 3`) {
		t.Fatal("async line not in format 3")
	}
}
