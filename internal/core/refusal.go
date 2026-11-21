package core

import (
	"encoding/json"
	"strings"

	"imageworkbench/internal/i18n"
)

// ---- why an image was refused, for the interface (0.8.0) ---------------------
//
// The interface used to tell a content-check refusal, an empty account and a
// rejected key apart by the Chinese start of Task.Error. Error text now comes in
// the display language it was written in (ADR 0004), so the task carries the
// kind as a code: Refusal is "policy", "balance", "auth" or "". It is worked out
// from the stored text in every language, so tasks saved earlier get it too.

// catalogPrefix reports whether text starts with the message key, up to its
// first placeholder, in any interface language.
func catalogPrefix(text, key string) bool {
	for _, v := range i18n.Variants(key) {
		if i := strings.Index(v, "{"); i >= 0 {
			v = v[:i]
		}
		if v != "" && strings.HasPrefix(text, v) {
			return true
		}
	}
	return false
}

func refusalOf(t Task) string {
	if t.Status != "failed" || t.Error == "" {
		return ""
	}
	switch {
	case catalogPrefix(t.Error, "generation.policyRefused"):
		return "policy"
	// 线路额度已用完: an empty account or a used-up trial allowance. Tasks saved
	// by earlier versions still carry the old "account out of balance" wording.
	case catalogPrefix(t.Error, "generation.balance"), catalogPrefix(t.Error, "generation.trialQuota"), catalogPrefix(t.Error, "generation.legacy.balance"):
		return "balance"
	case catalogPrefix(t.Error, "generation.keyRejected"):
		// Before 0.5.5 an empty account was reported as an invalid key.
		if balanceWords.MatchString(t.Error) {
			return "balance"
		}
		return "auth"
	}
	return ""
}

// MarshalJSON adds Refusal to the task sent to the interface.
func (t Task) MarshalJSON() ([]byte, error) {
	type plain Task
	return json.Marshal(struct {
		plain
		Refusal string `json:"refusal,omitempty"`
	}{plain(t), refusalOf(t)})
}
