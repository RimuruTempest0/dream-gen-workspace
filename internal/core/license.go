package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"imageworkbench/internal/i18n"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"imageworkbench/internal/license"
)

// A customer package carries customer.dat next to the program: its software ID,
// the customer's lines (trial and normal use share them) and a check value for
// the activation code. Without that file the app is in developer mode and
// behaves exactly as before.
//
// In trial only the main lines are used and a counter stops new images at the
// trial limit; the activation code unlocks normal use with the backup lines. The
// hard limit is the quota of each key at the upstream, raised after payment.

// licensePublicKey is the admin's Ed25519 public key (base64), set at build time
// with -ldflags "-X imageworkbench/internal/core.licensePublicKey=…".
var licensePublicKey = ""

// TrustLicenseKeyForTest makes this process trust pub (base64) for customer.dat,
// so the admin tool's tests can check that packages they sign activate in the
// app. It returns a function that restores the previous key.
func TrustLicenseKeyForTest(pub string) (restore func()) {
	old := licensePublicKey
	licensePublicKey = pub
	return func() { licensePublicKey = old }
}

type LicenseStatus struct {
	Mode           string `json:"mode"` // developer, trial, active, expired, invalid, mismatch
	SoftwareID     string `json:"softwareID,omitempty"`
	Customer       string `json:"customer,omitempty"`
	TrialLimit     int    `json:"trialLimit"`
	TrialUsed      int    `json:"trialUsed"`
	TrialPending   int    `json:"trialPending"`
	TrialRemaining int    `json:"trialRemaining"`
	// UpstreamRemaining is the main image line's remaining quota read from the
	// upstream (free self-query), e.g. "0.33 USD"; empty when unknown.
	UpstreamRemaining string `json:"upstreamRemaining,omitempty"`
	ActivatedAt       string `json:"activatedAt,omitempty"`
	Message           string `json:"message,omitempty"`
	// ExpiresAt is when the licence ends (empty: never); DaysLeft is set in the
	// last 7 days before it.
	ExpiresAt string `json:"expiresAt,omitempty"`
	DaysLeft  int    `json:"daysLeft,omitempty"`
	// Activated tells an expired licence that had been activated from one in trial.
	Activated bool `json:"activated,omitempty"`
	// Hosted: a mass-market program running as the hosted edition (customer.dat is
	// beside it). CanImport: a mass-market program in the open edition, which
	// becomes the hosted one by importing a licence file.
	Hosted    bool `json:"hosted,omitempty"`
	CanImport bool `json:"canImport,omitempty"`
}

// licenseRecord is saved in the data directory (preferences/license).
type licenseRecord struct {
	SoftwareID    string `json:"softwareID"`
	Activated     bool   `json:"activated"`
	ActivatedAt   string `json:"activatedAt,omitempty"`
	CodeRef       string `json:"codeRef,omitempty"` // DPAPI-sealed activation code
	LinesRevision int    `json:"linesRevision"`
	// SkillsApplied is the built-in skill selection last applied from customer.dat.
	SkillsApplied string `json:"skillsApplied,omitempty"`
}

type upstreamUsage struct {
	at        time.Time
	limited   bool
	remaining float64
	unit      string
	known     bool
	fetching  bool
}

type licenseState struct {
	profile *license.Profile
	mode    string
	message i18n.Message  // shown in the language current when the status is read
	shared  sharedLicense // counter and activation kept outside the data folder
	usage   upstreamUsage
}

const licensePrefKey = "license"

func (s *Service) licenseRecordLocked() (licenseRecord, bool) {
	var r licenseRecord
	return r, s.store.Get("preferences", licensePrefKey, &r) == nil
}

// licenseKey is the public key customer.dat must be signed with. The mass-market
// line trusts the key its updates are signed with (docs/adr/0003): its builds
// carry no licence key of their own.
func licenseKey() string {
	if productLine != "" {
		return updatePublicKey
	}
	return licensePublicKey
}

// hostedLocked reports whether a mass-market program runs as the hosted edition
// (托管版): customer.dat is beside it, whatever state the licence is in.
func (s *Service) hostedLocked() bool { return productLine != "" && s.lic.mode != "" }

// LoadLicense reads customer.dat from the program folder. A missing file means
// developer mode only in builds without a trusted public key; a customer build
// (key built in) without it is invalid, so removing the file never unlocks the
// app or the built-in skills. The mass-market line is one program for both of
// its editions: without the file it is the open edition, configured by its user,
// and the provider's lines of an earlier licence are taken away. Called once at
// start, before the queue runs.
func (s *Service) LoadLicense(programDir string) error {
	s.mu.Lock()
	s.programDir = programDir
	s.mu.Unlock()
	data, e := os.ReadFile(filepath.Join(programDir, license.FileName))
	if errors.Is(e, os.ErrNotExist) {
		if licensePublicKey == "" && productLine == "" {
			return nil
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		if productLine != "" {
			s.lic = licenseState{}
			return s.releaseHostedLocked()
		}
		s.lic = licenseState{mode: "invalid", message: missingCustomerFile}
		_ = s.store.appendLog("error", "license", "", s.text(missingCustomerFile))
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if e != nil {
		s.lic = licenseState{mode: "invalid", message: i18n.Msg("license.file.unreadable", "error", e)}
		return nil
	}
	if msg := programSignatureProblem(); !msg.IsZero() {
		s.lic = licenseState{mode: "invalid", message: msg}
		_ = s.store.appendLog("error", "license", "", s.text(msg))
		return nil
	}
	keys := []string{}
	if key := licenseKey(); key != "" {
		keys = append(keys, key)
	}
	p, e := license.Verify(data, keys)
	if e != nil {
		s.lic = licenseState{mode: "invalid", message: i18n.Msg("license.file.invalidPackage")}
		if productLine != "" {
			s.lic.message = invalidHostedFile
		}
		_ = s.store.appendLog("error", "license", "", s.text(s.lic.message))
		return nil
	}
	s.lic = licenseState{profile: &p, mode: "trial"}
	rec, exists := s.licenseRecordLocked()
	if exists && rec.SoftwareID != p.SoftwareID {
		s.lic.mode = "mismatch"
		s.lic.message = i18n.Msg("license.dataOfOther", "id", rec.SoftwareID)
		_ = s.store.appendLog("error", "license", "", s.text(s.lic.message))
		return nil
	}
	if !exists {
		rec = licenseRecord{SoftwareID: p.SoftwareID}
	}
	shared, tampered := loadShared(p.SoftwareID)
	if tampered {
		// An edited trial record ends the trial; a valid activation still counts.
		shared.Used = max(shared.Used, p.TrialImages)
		_ = s.store.appendLog("warn", "license", "", s.msg("license.log.trialTampered"))
	}
	s.lic.shared = shared
	s.noteRunningLocked(time.Now())
	// Activated here or by another copy of the same software; the saved code is
	// checked again, so a copied flag alone does not activate.
	code := ""
	if rec.Activated && rec.CodeRef != "" {
		if saved, e := s.key(rec.CodeRef); e == nil {
			code = saved
		}
	}
	if code == "" {
		code = shared.Code
	}
	if code != "" && license.CheckCode(p.CodeCheck, code, p.SoftwareID) == nil {
		s.lic.mode = "active"
		if !rec.Activated {
			if e = s.rememberActivationLocked(&rec, code); e != nil {
				return e
			}
		}
	} else if rec.Activated {
		rec.Activated, rec.CodeRef = false, ""
		s.lic.message = i18n.Msg("license.code.changed")
	}
	if e = s.applySkillSelectionLocked(&rec); e != nil {
		return e
	}
	if e = s.applyLinesLocked(&rec); e != nil {
		s.lic = licenseState{mode: "invalid", message: i18n.Msg("license.lines.invalid", "error", e)}
		return nil
	}
	return s.store.Put("preferences", licensePrefKey, rec)
}

// applyLinesLocked creates or updates the customer's lines and the gateway
// order: the main line only in trial, the backups once activated.
func (s *Service) applyLinesLocked(rec *licenseRecord) error {
	p := s.lic.profile
	if e := license.CheckLines(p.Connections, p.ImageBackups); e != nil {
		return e
	}
	ids := map[string]string{}
	cs, _ := List[Connection](s.store, "connection")
	for _, c := range cs {
		if c.Managed != "" {
			ids[c.Managed] = c.ID
		}
	}
	if productLine != "" && len(ids) == 0 {
		// The first licence on a program its user had configured: remember which
		// image services were in use, to give them back when the licence goes.
		var own GatewaySettings
		if s.store.Get("preferences", ownGatewaysPrefKey, &own) != nil {
			if e := s.store.Put("preferences", ownGatewaysPrefKey, s.gatewaySettingsLocked()); e != nil {
				return e
			}
		}
	}
	if rec.LinesRevision != p.LinesRevision || len(ids) < len(p.Connections) {
		for _, c := range p.Connections {
			key, e := license.RevealKey(c.KeyObf, p.SoftwareID)
			if e != nil {
				return e
			}
			c.Key = key
			saved, e := s.upsertManagedLocked(c)
			if e != nil {
				return e
			}
			ids[c.Slot] = saved.ID
		}
		rec.LinesRevision = p.LinesRevision
	}
	return s.setLicenseGatewaysLocked(ids)
}

func (s *Service) setLicenseGatewaysLocked(ids map[string]string) error {
	if ids == nil {
		ids = map[string]string{}
		cs, _ := List[Connection](s.store, "connection")
		for _, c := range cs {
			if c.Managed != "" {
				ids[c.Managed] = c.ID
			}
		}
	}
	g := GatewaySettings{PrimaryID: ids["image-1"], BackupIDs: []string{}, AutoReroute: true}
	if s.lic.mode == "active" {
		for _, slot := range s.lic.profile.ImageBackups {
			if id := ids[slot]; id != "" {
				g.BackupIDs = append(g.BackupIDs, id)
			}
		}
	}
	return s.store.Put("preferences", gatewaySettingsPrefKey, g)
}

// upsertManagedLocked creates or updates the line for a slot; the key is stored
// with DPAPI like any other connection key.
func (s *Service) upsertManagedLocked(c license.Connection) (Connection, error) {
	cs, e := List[Connection](s.store, "connection")
	if e != nil {
		return Connection{}, e
	}
	id := ""
	for _, existing := range cs {
		if existing.Managed == c.Slot {
			id = existing.ID
		}
	}
	saved, e := s.saveConnectionLocked(ConnectionInput{ID: id, Kind: c.Kind, Name: c.Name, BaseURL: c.BaseURL, Model: c.Model, Key: c.Key, Mode: c.Mode, AuthMode: c.AuthMode, MaxOutputTokens: c.MaxOutputTokens, Protocol: c.Protocol})
	if e != nil {
		return Connection{}, i18n.Fail("license.lines.save", "name", c.Name, "error", e)
	}
	var full Connection
	if e = s.store.Get("connection", saved.ID, &full); e != nil {
		return Connection{}, e
	}
	full.Managed = c.Slot
	if c.Kind == "llm" && c.Verified {
		full.Capabilities = LLMCapabilities{Model: full.Model, Text: true, Vision: true, Tools: true, Streaming: true, CheckedAt: Now(), Details: []string{s.msg("license.verifiedByProvider")}}
	}
	return full, s.store.Put("connection", full.ID, full)
}

const ownGatewaysPrefKey = "image-gateways-own"

// releaseHostedLocked turns a mass-market program without customer.dat back into
// the open edition: the provider's lines and their keys are deleted, so removing
// the file never leaves the provider's keys usable, and the user's own image
// services are in use again. The trial count and the activation are kept outside
// the data folder, so importing the same licence again does not reset them.
func (s *Service) releaseHostedLocked() error {
	cs, e := List[Connection](s.store, "connection")
	if e != nil {
		return e
	}
	removed := 0
	for _, c := range cs {
		if c.Managed == "" {
			continue
		}
		if c.Credential != "" {
			_ = os.Remove(filepath.Join(s.store.Root, "secrets", c.Credential+".bin"))
		}
		if e = s.store.Delete("connection", c.ID); e != nil {
			return e
		}
		removed++
	}
	_, had := s.licenseRecordLocked()
	if removed == 0 && !had {
		return nil
	}
	var own GatewaySettings
	_ = s.store.Get("preferences", ownGatewaysPrefKey, &own)
	if e = s.store.Put("preferences", gatewaySettingsPrefKey, own); e != nil {
		return e
	}
	_ = s.store.Delete("preferences", ownGatewaysPrefKey)
	_ = s.store.Delete("preferences", licensePrefKey)
	_ = s.store.appendLog("info", "license", "", s.msg("license.log.released"))
	return nil
}

func (s *Service) rememberActivationLocked(rec *licenseRecord, code string) error {
	sealed, e := Protect([]byte(code))
	if e != nil {
		return e
	}
	ref := NewID()
	if e = atomicWrite(filepath.Join(s.store.Root, "secrets", ref+".bin"), sealed); e != nil {
		return e
	}
	rec.Activated, rec.ActivatedAt, rec.CodeRef = true, Now(), ref
	s.lic.shared.Code = code
	_ = saveShared(s.lic.profile.SoftwareID, s.lic.shared)
	return nil
}

// Activate checks the code against this software and unlocks normal use with
// the backup lines. Images, tasks and projects made during the trial stay.
func (s *Service) Activate(code string) (LicenseStatus, error) {
	s.mu.Lock()
	switch s.lic.mode {
	case "", "developer":
		s.mu.Unlock()
		return LicenseStatus{}, s.fail("license.activate.notNeeded")
	case "invalid", "mismatch":
		msg := s.text(s.lic.message)
		s.mu.Unlock()
		return LicenseStatus{}, errors.New(msg)
	}
	p := s.lic.profile
	norm, e := license.NormalizeCode(code)
	if e == nil {
		e = license.CheckCode(p.CodeCheck, norm, p.SoftwareID)
	}
	if e != nil {
		s.mu.Unlock()
		_ = s.store.appendLog("warn", "license", "", s.msg("license.log.activateFailed", "error", s.errText(e)))
		return LicenseStatus{}, s.localErr(e)
	}
	rec, _ := s.licenseRecordLocked()
	if e = s.rememberActivationLocked(&rec, norm); e == nil {
		s.lic.mode, s.lic.message = "active", i18n.Message{}
		e = s.setLicenseGatewaysLocked(nil)
	}
	if e == nil {
		e = s.store.Put("preferences", licensePrefKey, rec)
	}
	if e != nil {
		s.mu.Unlock()
		return LicenseStatus{}, e
	}
	_ = s.store.appendLog("info", "license", "", s.msg("license.log.activated"))
	s.mu.Unlock()
	s.signal()
	return s.LicenseStatus(), nil
}

// trialUsage counts trial images by status: saved ones and those with an unknown
// result (possibly billed) are used; queued and in-flight ones are pending.
func trialUsage(counts map[string]int) (used, pending, inFlight int) {
	used = counts["succeeded"] + counts["downloading"] + counts["download_failed"] + counts["unknown"]
	inFlight = counts["submitting"] + counts["running"]
	return used, counts["queued"] + inFlight, inFlight
}

// trialCountLocked is the trial count across this data folder and the copies
// kept outside it; a higher local count is written back to those copies. It reads
// the indexed counts, not every task.
func (s *Service) trialCountLocked() (used, pending int) {
	counts, _ := s.store.taskStatusCounts(true)
	local, pending, _ := trialUsage(counts)
	if local > s.lic.shared.Used && s.lic.profile != nil {
		s.lic.shared.Used = local
		_ = saveShared(s.lic.profile.SoftwareID, s.lic.shared)
	}
	return max(local, s.lic.shared.Used), pending
}

func (s *Service) licenseStatusLocked() LicenseStatus {
	st := LicenseStatus{Mode: s.lic.mode, Message: s.text(s.lic.message), Hosted: s.hostedLocked()}
	if st.Mode == "" {
		st.Mode = "developer"
		st.CanImport = productLine != "" && updatePublicKey != "" && s.programDir != ""
	}
	if p := s.lic.profile; p != nil {
		st.SoftwareID, st.Customer, st.TrialLimit = p.SoftwareID, p.Customer, p.TrialImages
	}
	if rec, ok := s.licenseRecordLocked(); ok && rec.Activated {
		st.ActivatedAt = rec.ActivatedAt
	}
	if p := s.lic.profile; p != nil && p.ExpiresAt != "" && (st.Mode == "trial" || st.Mode == "active") {
		st.ExpiresAt = p.ExpiresAt
		st.Activated = st.Mode == "active"
		now := time.Now()
		s.noteRunningLocked(now)
		if expired, msg := s.expiredLocked(now); expired {
			st.Mode, st.Message = "expired", msg
		} else if at, e := time.Parse(time.RFC3339, p.ExpiresAt); e == nil && at.Sub(now) <= expiryWarning {
			st.DaysLeft = max(1, int(math.Ceil(at.Sub(now).Hours()/24)))
		}
	}
	if st.Mode == "trial" || st.Mode == "active" {
		// The main image line's remaining quota, in trial and after activation.
		s.refreshUsageLocked()
		if u := s.lic.usage; u.known && u.limited {
			st.UpstreamRemaining = fmt.Sprintf("%.2f %s", u.remaining, u.unit)
		}
	}
	if st.Mode == "trial" {
		st.TrialUsed, st.TrialPending = s.trialCountLocked()
		st.TrialRemaining = max(0, st.TrialLimit-st.TrialUsed-st.TrialPending)
		if u := s.lic.usage; u.known && u.limited && u.remaining <= 0 {
			// The upstream quota is the hard limit: when it is spent, so is the trial.
			st.TrialRemaining = 0
		}
	}
	return st
}

func (s *Service) LicenseStatus() LicenseStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.licenseStatusLocked()
}

// refreshUsageLocked reads the main image line's quota from the upstream in the
// background, at most once a minute (Sub2API /v1/usage, not billed).
func (s *Service) refreshUsageLocked() {
	u := &s.lic.usage
	if u.fetching || time.Since(u.at) < time.Minute || s.lic.profile == nil {
		return
	}
	var line license.Connection
	for _, c := range s.lic.profile.Connections {
		if c.Slot == "image-1" {
			line = c
		}
	}
	if line.UsageURL == "" {
		return
	}
	key, e := license.RevealKey(line.KeyObf, s.lic.profile.SoftwareID)
	if e != nil {
		return
	}
	u.fetching = true
	go func() {
		limited, remaining, unit, ok := fetchUsage(s.ctx, line.UsageURL, key)
		s.mu.Lock()
		next := upstreamUsage{at: time.Now(), limited: limited, remaining: remaining, unit: unit, known: ok}
		s.recordUsageLocked(s.lic.usage, next)
		if !ok && s.lic.usage.known {
			// Keep the last reading for the cost measurement; retry in a minute.
			next = s.lic.usage
			next.at, next.fetching = time.Now(), false
		}
		s.lic.usage = next
		s.mu.Unlock()
		s.signal()
	}()
}

func fetchUsage(ctx context.Context, url, key string) (limited bool, remaining float64, unit string, ok bool) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, e := http.NewRequestWithContext(ctx, "GET", url, nil)
	if e != nil {
		return
	}
	req.Header.Set("Authorization", "Bearer "+key)
	resp, e := (&http.Client{Transport: newNetworkTransport()}).Do(req)
	if e != nil {
		return
	}
	defer resp.Body.Close()
	var v struct {
		Mode  string `json:"mode"`
		Quota struct {
			Remaining float64 `json:"remaining"`
			Unit      string  `json:"unit"`
		} `json:"quota"`
	}
	if resp.StatusCode != 200 || json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&v) != nil {
		return
	}
	return v.Mode == "quota_limited", v.Quota.Remaining, v.Quota.Unit, true
}

func (s *Service) trialLocked() bool { return s.lic.mode == "trial" }

var (
	missingCustomerFile = i18n.Msg("license.file.missing")
	invalidHostedFile   = i18n.Msg("license.file.invalidHosted")
	hostedLinesOnly     = i18n.Msg("license.hostedLinesOnly")
	trialUsedUp         = i18n.Msg("license.trial.usedUp")
	trialLinesOnly      = i18n.Msg("license.trial.linesOnly")
)

// trialLineLocked reports whether a connection may be used during the trial: only
// the main lines, so backup keys are spent only after activation.
func (s *Service) trialLineLocked(id string) bool {
	var c Connection
	return s.store.Get("connection", id, &c) == nil && mainLine(c)
}

func mainLine(c Connection) bool { return c.Managed == "image-1" || c.Managed == "llm-1" }

// ensureCanGenerateLocked refuses new images when the licence blocks generation,
// the trial has fewer images left than requested, or a trial request names a line
// other than the main ones.
func (s *Service) ensureCanGenerateLocked(n int, lines ...string) error {
	if expired, msg := s.expiredLocked(time.Now()); expired {
		return errors.New(msg)
	}
	if s.hostedLocked() {
		for _, id := range lines {
			var c Connection
			if id != "" && s.store.Get("connection", id, &c) == nil && c.Managed == "" {
				return errors.New(s.text(hostedLinesOnly))
			}
		}
	}
	switch s.lic.mode {
	case "invalid", "mismatch":
		return errors.New(s.text(s.lic.message))
	case "trial":
		for _, id := range lines {
			if !s.trialLineLocked(id) {
				return errors.New(s.text(trialLinesOnly))
			}
		}
		st := s.licenseStatusLocked()
		if st.TrialRemaining <= 0 {
			return errors.New(s.text(trialUsedUp))
		}
		if n > st.TrialRemaining {
			return s.fail("license.trial.notEnough", "left", st.TrialRemaining, "n", n)
		}
	}
	return nil
}

// trialStopLocked is the last check before an image is sent: in trial, nothing
// beyond the limit leaves the app even if it was queued earlier.
func (s *Service) trialStopLocked() bool {
	if expired, _ := s.expiredLocked(time.Now()); expired {
		return true
	}
	if s.lic.mode != "trial" {
		return s.lic.mode == "invalid" || s.lic.mode == "mismatch"
	}
	used, _ := s.trialCountLocked()
	counts, _ := s.store.taskStatusCounts(true)
	_, _, inFlight := trialUsage(counts)
	return used+inFlight >= s.lic.profile.TrialImages
}

func (s *Service) refuseManagedLocked(id string) error {
	if id == "" {
		return nil
	}
	var c Connection
	if s.store.Get("connection", id, &c) == nil && c.Managed != "" && s.lic.mode != "" {
		return s.fail("license.managedLine")
	}
	return nil
}

// licenseBlockMessageLocked explains why nothing can be generated, or "".
func (s *Service) licenseBlockMessageLocked() string {
	if expired, msg := s.expiredLocked(time.Now()); expired {
		return msg
	}
	if s.lic.mode == "invalid" || s.lic.mode == "mismatch" {
		return strings.TrimSpace(s.text(s.lic.message))
	}
	return ""
}
