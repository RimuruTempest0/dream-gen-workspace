package core

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"time"
)

// Several image relays can be configured: one primary and ordered backups. The
// app watches their health without paid calls and moves images that were never
// sent to a healthy backup. Re-generating an image whose result is unknown (HTTP
// 524) can be billed twice, so it only happens when the user turned it on.

type GatewaySettings struct {
	PrimaryID string   `json:"primaryID"`
	BackupIDs []string `json:"backupIDs"`
	// AutoReroute moves unsent images to a healthy backup (no extra cost).
	AutoReroute bool `json:"autoReroute"`
	// AutoRegenerate524 re-generates an image cut by a relay timeout once, which
	// may be billed twice. Off unless the user confirmed the cost notice.
	AutoRegenerate524 bool `json:"autoRegenerate524"`
}

type GatewayHealth struct {
	ConnectionID   string `json:"connectionID"`
	Name           string `json:"name"`
	Model          string `json:"model"`
	State          string `json:"state"` // healthy, slow, down, unknown
	Reason         string `json:"reason"`
	CheckedAt      string `json:"checkedAt,omitempty"`
	ProbeMS        int64  `json:"probeMS"`
	ModelsOK       bool   `json:"modelsOK"`
	ModelCount     int    `json:"modelCount"`
	MedianMS       int64  `json:"medianMS"`
	RecentTimeouts int    `json:"recentTimeouts"`
	InFlight       int    `json:"inFlight"`
}

// Health thresholds; package variables so tests can shorten them.
var (
	slowGenerationMS   int64 = 90_000
	healthWindow             = 15 * time.Minute
	recoverQuiet             = 10 * time.Minute
	probeInterval            = 3 * time.Minute
	downProbeInterval        = 30 * time.Second
	probeTimeout             = 30 * time.Second
	modelListFreshness       = 30 * time.Minute
)

const (
	maxBackupGateways      = 5
	maxAutoRegenPerBatch   = 3
	gatewaySettingsPrefKey = "image-gateways"
)

type probeResult struct {
	at     time.Time
	ms     int64
	ok     bool // model list read
	down   bool
	reason string
	models []string
}

func (s *Service) gatewaySettingsLocked() GatewaySettings {
	g := GatewaySettings{AutoReroute: true}
	_ = s.store.Get("preferences", gatewaySettingsPrefKey, &g)
	if g.BackupIDs == nil {
		g.BackupIDs = []string{}
	}
	return g
}

func (s *Service) GetGatewaySettings() (GatewaySettings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.gatewaySettingsLocked(), nil
}

func (s *Service) SetGatewaySettings(g GatewaySettings) error {
	s.mu.Lock()
	image := func(id string) error {
		var c Connection
		if e := s.store.Get("connection", id, &c); e != nil {
			return missing(e, "所选生图中转站已删除，请刷新")
		}
		if normalizeConnection(c).Kind != "image" {
			return errors.New("只能选择生图中转站")
		}
		return nil
	}
	if g.PrimaryID != "" {
		if e := image(g.PrimaryID); e != nil {
			s.mu.Unlock()
			return e
		}
	}
	if len(g.BackupIDs) > maxBackupGateways {
		s.mu.Unlock()
		return fmt.Errorf("最多设置 %d 个备用中转站", maxBackupGateways)
	}
	backups := []string{}
	for _, id := range g.BackupIDs {
		if id == g.PrimaryID || slices.Contains(backups, id) {
			continue
		}
		if e := image(id); e != nil {
			s.mu.Unlock()
			return e
		}
		backups = append(backups, id)
	}
	g.BackupIDs = backups
	old := s.gatewaySettingsLocked()
	if e := s.store.Put("preferences", gatewaySettingsPrefKey, g); e != nil {
		s.mu.Unlock()
		return e
	}
	if g.AutoRegenerate524 != old.AutoRegenerate524 {
		text := "已关闭 524 自动重新生成"
		if g.AutoRegenerate524 {
			text = "已开启 524 自动重新生成（用户已确认可能重复计费）"
		}
		_ = s.store.appendLog("info", "image", "", text)
	}
	if g.AutoReroute != old.AutoReroute {
		_ = s.store.appendLog("info", "image", "", fmt.Sprintf("自动转到备用中转站：%v", g.AutoReroute))
	}
	s.mu.Unlock()
	s.signal()
	return nil
}

// removeGatewayLocked drops a deleted connection from the settings.
func (s *Service) removeGatewayLocked(id string) {
	g := s.gatewaySettingsLocked()
	if g.PrimaryID == id {
		g.PrimaryID = ""
	}
	g.BackupIDs = slices.DeleteFunc(g.BackupIDs, func(v string) bool { return v == id })
	_ = s.store.Put("preferences", gatewaySettingsPrefKey, g)
	delete(s.probes, id)
}

// ---- free health probe -------------------------------------------------------

func httpStatusOf(e error) int {
	var n int
	if e != nil {
		fmt.Sscanf(e.Error(), "HTTP %d", &n)
	}
	return n
}

// probe reads the model list, which never generates or bills.
func (s *Service) probe(id string) {
	s.mu.Lock()
	var c Connection
	if s.store.Get("connection", id, &c) != nil || normalizeConnection(c).Kind != "image" {
		delete(s.probing, id)
		s.mu.Unlock()
		return
	}
	key, keyErr := s.key(c.Credential)
	s.mu.Unlock()
	r := probeResult{at: time.Now()}
	if keyErr != nil {
		r.down, r.reason = true, "没有可用的 API Key"
	} else {
		ctx, cancel := context.WithTimeout(s.ctx, probeTimeout)
		start := time.Now()
		ids, e := s.provider.Models(ctx, normalizeConnection(c), key)
		cancel()
		r.ms = time.Since(start).Milliseconds()
		status := httpStatusOf(e)
		switch {
		case s.ctx.Err() != nil:
			s.mu.Lock()
			delete(s.probing, id)
			s.mu.Unlock()
			return
		case e == nil:
			r.ok, r.models = true, ids
			s.saveModels(id, "", ids)
		case status == 401 || status == 403:
			r.down, r.reason = true, fmt.Sprintf("API Key 被拒绝（HTTP %d）", status)
		case status >= 500:
			r.down, r.reason = true, fmt.Sprintf("中转站异常（HTTP %d）", status)
		case status == 0:
			r.down, r.reason = true, "无法连接中转站："+redact(e.Error(), key)
		default:
			// Some relays do not list models; generation may still work, but the
			// exact model cannot be verified, so it is not used as a backup.
			r.reason = "无法读取模型列表，不能作为备用：" + redact(e.Error(), key)
		}
	}
	s.mu.Lock()
	prev, had := s.probes[id]
	s.probes[id] = r
	delete(s.probing, id)
	if r.down && (!had || !prev.down) {
		_ = s.store.appendLog("warn", "image", id, fmt.Sprintf("免费检查：生图中转站「%s」暂时不可用（%s）", c.Name, r.reason))
	} else if !r.down && had && prev.down {
		_ = s.store.appendLog("info", "image", id, fmt.Sprintf("免费检查：生图中转站「%s」已恢复可连接", c.Name))
	}
	s.mu.Unlock()
	s.signal()
}

// maybeProbeLocked refreshes, in the background, the health of the gateways
// that waiting images may use. It only runs while there is work in the queue.
func (s *Service) maybeProbeLocked(tasks []Task) {
	ids := []string{}
	for _, t := range tasks {
		if (t.Status == "queued" || t.Status == "submitting") && !slices.Contains(ids, t.Connection.ID) {
			ids = append(ids, t.Connection.ID)
		}
	}
	g := s.gatewaySettingsLocked()
	// Health checks start once the user set up a primary or backup gateway.
	if len(ids) == 0 || (g.PrimaryID == "" && len(g.BackupIDs) == 0) {
		return
	}
	for _, id := range append([]string{g.PrimaryID}, g.BackupIDs...) {
		if id != "" && !slices.Contains(ids, id) {
			ids = append(ids, id)
		}
	}
	now := time.Now()
	for _, id := range ids {
		r, had := s.probes[id]
		every := probeInterval
		if had && r.down {
			every = downProbeInterval
		}
		if s.probing[id] || (had && now.Sub(r.at) < every) || s.ctx.Err() != nil {
			continue
		}
		s.probing[id] = true
		s.workers.Add(1)
		go func(id string) {
			defer s.workers.Done()
			s.probe(id)
		}(id)
	}
}

// CheckGateways runs the free check on every image relay now and waits for it.
func (s *Service) CheckGateways() ([]GatewayHealth, error) {
	s.mu.Lock()
	cs, e := List[Connection](s.store, "connection")
	if e != nil {
		s.mu.Unlock()
		return nil, e
	}
	ids := []string{}
	for _, c := range cs {
		if normalizeConnection(c).Kind == "image" && !s.probing[c.ID] {
			s.probing[c.ID] = true
			ids = append(ids, c.ID)
		}
	}
	s.mu.Unlock()
	done := make(chan struct{}, len(ids))
	for _, id := range ids {
		go func(id string) { s.probe(id); done <- struct{}{} }(id)
	}
	for range ids {
		<-done
	}
	return s.GatewayHealth()
}

// ---- health from probes and from the app's own generation history ------------

type outcome struct {
	at       time.Time
	ms       int64
	success  bool
	timeout  bool
	rejected string // auth or balance: the relay refused before generating
}

func taskOutcome(t Task) (outcome, bool) {
	start, e := time.Parse(time.RFC3339Nano, t.Diagnostic.StartedAt)
	if e != nil || t.Diagnostic.ElapsedMS <= 0 {
		return outcome{}, false
	}
	o := outcome{at: start.Add(time.Duration(t.Diagnostic.ElapsedMS) * time.Millisecond), ms: t.Diagnostic.ElapsedMS}
	switch t.Status {
	case "succeeded", "downloading", "download_failed":
		o.success = true
	case "unknown":
		o.timeout = relayTimedOut(t.Diagnostic)
	case "failed":
		o.rejected = rejectionKind(t.Diagnostic.HTTPStatus, t.Error)
	}
	return o, o.success || o.timeout || o.rejected != ""
}

func (s *Service) inFlightOnLocked(id string) int {
	n := 0
	for _, c := range s.genGateway {
		if c == id {
			n++
		}
	}
	return n
}

func (s *Service) healthLocked(c Connection, tasks []Task, now time.Time) GatewayHealth {
	h := GatewayHealth{ConnectionID: c.ID, Name: c.Name, Model: c.Model, State: "unknown", InFlight: s.inFlightOnLocked(c.ID)}
	outcomes := []outcome{}
	for _, t := range tasks {
		if t.Connection.ID != c.ID {
			continue
		}
		if o, ok := taskOutcome(t); ok && now.Sub(o.at) <= healthWindow {
			outcomes = append(outcomes, o)
		}
	}
	sort.Slice(outcomes, func(i, j int) bool { return outcomes[i].at.After(outcomes[j].at) })
	var lastTimeout, lastFast, lastSuccess, lastRejected time.Time
	rejectedFor := ""
	var timeoutMS int64
	recent := []int64{}
	for _, o := range outcomes {
		switch {
		case o.timeout:
			h.RecentTimeouts++
			if lastTimeout.IsZero() {
				lastTimeout, timeoutMS = o.at, o.ms
			}
		case o.rejected != "":
			if lastRejected.IsZero() {
				lastRejected, rejectedFor = o.at, o.rejected
			}
		case o.success:
			if lastSuccess.IsZero() {
				lastSuccess = o.at
			}
			if lastFast.IsZero() && o.ms <= slowGenerationMS {
				lastFast = o.at
			}
			if len(recent) < 5 {
				recent = append(recent, o.ms)
			}
		}
	}
	slowCount := 0
	for _, ms := range recent {
		if ms > slowGenerationMS {
			slowCount++
		}
	}
	if len(recent) > 0 {
		sorted := slices.Clone(recent)
		slices.Sort(sorted)
		h.MedianMS = sorted[len(sorted)/2]
	}
	p, probed := s.probes[c.ID]
	if probed {
		h.CheckedAt = p.at.UTC().Format(time.RFC3339Nano)
		h.ProbeMS = p.ms
		h.ModelsOK = p.ok
		h.ModelCount = len(p.models)
		if !p.ok && !p.down {
			h.Reason = p.reason
		}
	}
	switch {
	case probed && p.down && !lastSuccess.After(p.at):
		h.State, h.Reason = "down", p.reason
	// A refused key or an empty account stays down for a while: the free model
	// list cannot tell whether an account has balance again.
	case !lastRejected.IsZero() && now.Sub(lastRejected) < recoverQuiet && !lastSuccess.After(lastRejected):
		h.State, h.Reason = "down", rejectionLabels[rejectedFor]
	case !lastTimeout.IsZero() && (now.Sub(lastTimeout) < recoverQuiet || !lastFast.After(lastTimeout)):
		h.State, h.Reason = "slow", fmt.Sprintf("最近有图片在约 %d 秒后被中转站断开", timeoutMS/1000)
	case slowCount >= 2:
		h.State, h.Reason = "slow", fmt.Sprintf("最近图片生成偏慢（中位约 %d 秒）", h.MedianMS/1000)
	case (probed && p.ok) || !lastSuccess.IsZero():
		h.State = "healthy"
	}
	return h
}

func (s *Service) GatewayHealth() ([]GatewayHealth, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cs, e := List[Connection](s.store, "connection")
	if e != nil {
		return nil, e
	}
	tasks, e := List[Task](s.store, "task")
	if e != nil {
		return nil, e
	}
	out := []GatewayHealth{}
	now := time.Now()
	for _, c := range cs {
		if normalizeConnection(c).Kind == "image" {
			out = append(out, s.healthLocked(c, tasks, now))
		}
	}
	return out, nil
}

// ---- routing -----------------------------------------------------------------

type healthCache struct {
	s     *Service
	tasks []Task
	now   time.Time
	seen  map[string]GatewayHealth
}

func (h *healthCache) get(c Connection) GatewayHealth {
	if v, ok := h.seen[c.ID]; ok {
		v.InFlight = h.s.inFlightOnLocked(c.ID)
		return v
	}
	v := h.s.healthLocked(c, h.tasks, h.now)
	h.seen[c.ID] = v
	return v
}

// backupLocked returns the first healthy gateway, other than the task's own,
// whose free model list contains the task's exact model and that has room.
func (s *Service) backupLocked(t Task, g GatewaySettings, hc *healthCache) (Connection, bool) {
	for _, id := range append([]string{g.PrimaryID}, g.BackupIDs...) {
		if id == "" || id == t.Connection.ID {
			continue
		}
		var c Connection
		if s.store.Get("connection", id, &c) != nil || normalizeConnection(c).Kind != "image" || c.Credential == "" {
			continue
		}
		p, probed := s.probes[id]
		if !probed || !p.ok || hc.now.Sub(p.at) > modelListFreshness || !slices.Contains(p.models, t.Model) {
			continue
		}
		h := hc.get(c)
		if h.State != "healthy" || h.InFlight >= s.imageConcurrency {
			continue
		}
		return normalizeConnection(c), true
	}
	return Connection{}, false
}

var stateLabels = map[string]string{"slow": "变慢", "down": "不可用"}

// routeLocked decides whether a queued, never-sent task may be sent now. It may
// move it to a backup gateway (saved) or pause it unsent when its gateway is down.
func (s *Service) routeLocked(t *Task, g GatewaySettings, hc *healthCache) bool {
	// Without a primary or backup set up, images are sent exactly as before.
	if g.PrimaryID == "" && len(g.BackupIDs) == 0 {
		return true
	}
	h := hc.get(t.Connection)
	retry := t.AutoRetryOf != ""
	if h.State != "slow" && h.State != "down" {
		return true
	}
	if g.AutoReroute || retry {
		if b, ok := s.backupLocked(*t, g, hc); ok {
			from := t.Connection
			if t.RoutedFrom == "" {
				t.RoutedFrom, t.RoutedFromName = from.ID, from.Name
			}
			t.RouteReason = stateLabels[h.State] + "：" + h.Reason
			t.Connection = b
			t.UpdatedAt = Now()
			if s.store.Put("task", t.ID, *t) != nil {
				return false
			}
			_ = s.store.appendLog("info", "image", t.BatchID, fmt.Sprintf("图片尚未提交，已从「%s」（%s）转到备用中转站「%s」，不额外计费", from.Name, stateLabels[h.State], b.Name))
			return true
		}
	}
	// Without a backup, a slow gateway keeps one image at a time as a trial (no
	// paid test call); an automatic re-generation never goes into the slow period.
	if h.State == "slow" && !retry && h.InFlight == 0 {
		return true
	}
	if h.State == "down" && !retry {
		t.Status = "blocked"
		t.Error = fmt.Sprintf("生图中转站「%s」暂时不可用（%s），也没有可用的同模型备用中转站。此图尚未提交、未计费，恢复后可点“恢复生成”。", t.Connection.Name, h.Reason)
		t.UpdatedAt = Now()
		_ = s.store.Put("task", t.ID, *t)
		_ = s.store.appendLog("warn", "image", t.BatchID, t.Error)
	}
	return false
}

// ---- auto re-generate after a relay timeout (opt-in) --------------------------

// autoRegenerateLocked queues one more attempt for an image whose result is
// unknown after a relay timeout. It returns the note for the original image.
func (s *Service) autoRegenerateLocked(t *Task) string {
	g := s.gatewaySettingsLocked()
	if !g.AutoRegenerate524 {
		return "程序不会自动重发生图；可在“连接与设置 → 备用切换”开启自动换中转站重新生成。"
	}
	if t.AutoRetryOf != "" {
		return "这已是自动重新生成的图片，不会再次自动重发。"
	}
	tasks, e := List[Task](s.store, "task")
	if e != nil {
		return "程序不会自动重发生图。"
	}
	used := 0
	for _, o := range tasks {
		if o.BatchID == t.BatchID && o.AutoRetryOf != "" {
			used++
		}
	}
	if used >= maxAutoRegenPerBatch {
		return fmt.Sprintf("本批自动重新生成已达 %d 张上限，未再自动重发。", maxAutoRegenPerBatch)
	}
	hc := &healthCache{s: s, tasks: tasks, now: time.Now(), seen: map[string]GatewayHealth{}}
	target, onBackup := s.backupLocked(*t, g, hc)
	if !onBackup {
		var live Connection
		if s.store.Get("connection", t.Connection.ID, &live) != nil {
			return "原中转站已删除，未自动重发。"
		}
		target = normalizeConnection(live)
	}
	next, e := s.queueAttemptLocked(*t, target, t.ID)
	if e != nil {
		return "自动重新生成未能排队：" + e.Error()
	}
	t.AutoRetriedAs = next.ID
	_ = s.store.appendLog("warn", "image", t.BatchID, fmt.Sprintf("524 自动重新生成：%s → %s（「%s」，可能重复计费）", t.ID, next.ID, target.Name))
	if onBackup {
		return fmt.Sprintf("已自动转到「%s」重新生成一次（新的计费请求）。原请求可能已在「%s」生成并计费，这张图可能产生两次费用。", target.Name, t.Connection.Name)
	}
	return fmt.Sprintf("没有可用的同模型备用中转站，将在「%s」恢复正常后自动重新生成一次（新的计费请求）。原请求可能已计费，这张图可能产生两次费用。", target.Name)
}

// ---- relay rejections -------------------------------------------------------

var balanceWords = regexp.MustCompile(`(?i)insufficient[ _-]?(balance|quota|credit|funds)|quota[ _-]?(exhausted|exceeded)|\bbalance\b|\bquota\b|credits?\b|payment required|billing|余额|额度|欠费|充值`)

var rejectionLabels = map[string]string{"balance": "账户余额不足", "auth": "API Key 被拒绝"}

// rejectionKind tells an empty account ("balance") from a refused key ("auth")
// for a request the relay refused before generating. Relays answer both with
// 401/403 (sometimes 402 or 429), so the message decides.
func rejectionKind(status int, message string) string {
	switch status {
	case 402:
		return "balance"
	case 401, 403, 429:
		if balanceWords.MatchString(message) {
			return "balance"
		}
		if status != 429 {
			return "auth"
		}
	}
	return ""
}

// gatewaySetupLocked reports whether a primary or backup gateway is set up, which
// turns on health tracking and routing.
func (s *Service) gatewaySetupLocked() bool {
	g := s.gatewaySettingsLocked()
	return g.PrimaryID != "" || len(g.BackupIDs) > 0
}
