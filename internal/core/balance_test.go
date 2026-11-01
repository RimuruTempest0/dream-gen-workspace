package core

import (
	"strings"
	"testing"
)

func TestRejectionKindTellsBalanceFromKey(t *testing.T) {
	cases := []struct {
		status  int
		message string
		want    string
	}{
		{403, "HTTP 403：insufficient balance", "balance"},
		{403, "账户余额不足，请充值", "balance"},
		{402, "", "balance"},
		{429, "You exceeded your current quota, please check your plan and billing details", "balance"},
		{401, "HTTP 401：insufficient_quota", "balance"},
		{401, "HTTP 401：Invalid API key", "auth"},
		{403, "HTTP 403：access denied", "auth"},
		// Sub2API (幻灵, 1 Route) when a key reaches its own quota:
		{429, "HTTP 429：API_KEY_QUOTA_EXHAUSTED", "balance"},
		{429, "HTTP 429：api key quota exhausted", "balance"},
		{429, "HTTP 429：rate limit exceeded, retry later", ""},
		{500, "insufficient balance", ""},
	}
	for _, c := range cases {
		if got := rejectionKind(c.status, c.message); got != c.want {
			t.Errorf("%d %q → %q, want %q", c.status, c.message, got, c.want)
		}
	}
}

func TestEmptyAccountPausesTheBatchWithoutAskingForAKey(t *testing.T) {
	s := testService(t)
	g := &fakeGateway{models: []string{"test-image"}, refuse: 403, refuseMessage: "insufficient balance"}
	a := gatewayConnection(t, s, "1route", newFakeGateway(t, g).URL+"/v1")
	s.SetImageConcurrency(1)
	submitN(t, s, a, 3)
	s.Start(func() {})
	waitFor(t, "batch paused", func() bool { return countStatus(s, "failed") == 1 && countStatus(s, "blocked") == 2 })
	if g.imageHits.Load() != 1 {
		t.Fatal("images were sent to an empty account", g.imageHits.Load())
	}
	failed := tasksWhere(s, func(t Task) bool { return t.Status == "failed" })[0]
	if !strings.HasPrefix(failed.Error, "中转站账户余额不足（HTTP 403）") || strings.Contains(failed.Error, "API Key 无效") || !strings.Contains(failed.Error, "insufficient balance") {
		t.Fatal(failed.Error)
	}
	paused := tasksWhere(s, func(t Task) bool { return t.Status == "blocked" })[0]
	if !strings.Contains(paused.Error, "余额不足") || !strings.Contains(paused.Error, "充值") || strings.Contains(paused.Error, "密钥") {
		t.Fatal(paused.Error)
	}
	m, _ := s.GatewayMonitor(MonitorQuery{Days: 1})
	if m.FailedBalance != 1 || m.FailedAuth != 0 || m.Calls[0].FailReason != "balance" {
		t.Fatalf("monitor: %+v", m)
	}
}

func TestEmptyPrimaryMovesUnsentImagesToBackup(t *testing.T) {
	s := testService(t)
	empty := &fakeGateway{models: []string{"test-image"}, refuse: 403, refuseMessage: "insufficient balance"}
	backup := &fakeGateway{models: []string{"test-image"}}
	a := gatewayConnection(t, s, "A", newFakeGateway(t, empty).URL+"/v1")
	b := gatewayConnection(t, s, "B", newFakeGateway(t, backup).URL+"/v1")
	s.SetGatewaySettings(GatewaySettings{PrimaryID: a.ID, BackupIDs: []string{b.ID}, AutoReroute: true})
	s.SetImageConcurrency(1)
	s.CheckGateways() // the model list still works: only images are refused
	submitN(t, s, a, 3)
	s.Start(func() {})
	waitFor(t, "rest made on the backup", func() bool { return countStatus(s, "succeeded") == 2 })
	if empty.imageHits.Load() != 1 || backup.imageHits.Load() != 2 || countStatus(s, "blocked") != 0 {
		t.Fatal("unsent images must move to the backup", empty.imageHits.Load(), backup.imageHits.Load())
	}
	health, _ := s.GatewayHealth()
	for _, h := range health {
		if h.ConnectionID == a.ID && (h.State != "down" || h.Reason != "账户余额不足") {
			t.Fatalf("empty gateway health: %+v", h)
		}
	}
}

func TestEmptyPrimaryWithoutBackupPausesAndSendsNoMore(t *testing.T) {
	s := testService(t)
	empty := &fakeGateway{models: []string{"test-image"}, refuse: 403, refuseMessage: "insufficient balance"}
	a := gatewayConnection(t, s, "A", newFakeGateway(t, empty).URL+"/v1")
	s.SetGatewaySettings(GatewaySettings{PrimaryID: a.ID, AutoReroute: true})
	s.SetImageConcurrency(1)
	s.CheckGateways()
	submitN(t, s, a, 3)
	s.Start(func() {})
	waitFor(t, "unsent images paused", func() bool { return countStatus(s, "blocked") == 2 })
	if empty.imageHits.Load() != 1 {
		t.Fatal("more images sent to an empty account", empty.imageHits.Load())
	}
	paused := tasksWhere(s, func(t Task) bool { return t.Status == "blocked" })[0]
	if !strings.Contains(paused.Error, "账户余额不足") || !strings.Contains(paused.Error, "未提交、未计费") {
		t.Fatal(paused.Error)
	}
}
