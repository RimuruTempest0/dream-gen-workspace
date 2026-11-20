package admin

import (
	"strings"
	"testing"
)

// Keys cancelled at the upstream form their own category: never assigned or
// re-enabled, but their records still stop the same key being imported again.
func TestRevokedKeysStayAsRecordsOnly(t *testing.T) {
	a := open(t)
	stock(t, a, 1, "")
	repo(t, a)
	c, _ := a.CreateCustomer("客户 F", 0)
	a.AutoAssign(c.ID)
	if _, e := buildPackage(a, c.ID, "0.8.17", func(string) {}); e != nil {
		t.Fatal(e)
	}
	u := a.State().Upstreams[0].ID
	if r, _ := a.ImportKeys(u, "primary", "sk-revoke-key-0001\nsk-revoke-key-0002", ""); r.Added != 2 {
		t.Fatal(r)
	}
	var spare []string
	var delivered, assigned string
	for _, k := range a.State().Keys {
		switch {
		case strings.HasPrefix(k.Tail, "000"):
			spare = append(spare, k.ID)
		case k.Slot == "image-1":
			delivered = k.ID
		}
	}
	// Swap image-1: its delivered key comes off (disabled), a spare goes on.
	if e := a.AssignKey(c.ID, "image-1", spare[1]); e != nil {
		t.Fatal(e)
	}
	assigned = spare[1]
	status := func(id string) PoolKey {
		for _, k := range a.State().Keys {
			if k.ID == id {
				return k
			}
		}
		t.Fatal("no key", id)
		return PoolKey{}
	}

	// An assigned key cannot be revoked, and the whole selection stays as it was.
	if e := a.RevokeKeys([]string{delivered, assigned}); e == nil || !strings.Contains(e.Error(), "正分配给顾客") {
		t.Fatal("revoked an assigned key:", e)
	}
	if status(delivered).Status != "disabled" {
		t.Fatal("a refused revoke changed a key")
	}
	// A delivered (disabled) key and a never-used one are revoked together.
	if e := a.RevokeKeys([]string{delivered, spare[0]}); e != nil {
		t.Fatal(e)
	}
	for _, id := range []string{delivered, spare[0]} {
		if k := status(id); k.Status != "revoked" || k.RevokedAt == "" {
			t.Fatalf("not revoked: %+v", k)
		}
	}
	// Revoked keys are never handed out or re-enabled, and disabling keeps them revoked.
	if e := a.EnableKey(delivered); e == nil {
		t.Fatal("re-enabled a revoked key")
	}
	if e := a.AssignKey(c.ID, "image-2", spare[0]); e == nil {
		t.Fatal("assigned a revoked key")
	}
	if e := a.DisableKeys([]string{spare[0]}, "手动停用"); e != nil || status(spare[0]).Status != "revoked" {
		t.Fatal("disabling took a key out of revoked:", e)
	}
	// The record still blocks importing the same key again.
	if r, _ := a.ImportKeys(u, "primary", "sk-revoke-key-0001", ""); r.Added != 0 || r.Duplicates != 1 {
		t.Fatal("a revoked key was imported again", r)
	}
	// A delivered one still cannot be deleted.
	if e := a.DeleteKeys([]string{delivered}); e == nil {
		t.Fatal("deleted a delivered revoked key")
	}
	// Marked by mistake: back to disabled, re-enabled only from there.
	if e := a.UnrevokeKeys([]string{delivered, assigned}); e == nil {
		t.Fatal("unrevoked a key that was not revoked")
	}
	if e := a.UnrevokeKeys([]string{spare[0]}); e != nil {
		t.Fatal(e)
	}
	if k := status(spare[0]); k.Status != "disabled" || k.RevokedAt != "" {
		t.Fatalf("not back to disabled: %+v", k)
	}
}
