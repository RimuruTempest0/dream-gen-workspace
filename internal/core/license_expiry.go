package core

import (
	"os"
	"path/filepath"
	"time"

	"imageworkbench/internal/license"
)

// A licence with an expiry date (customer.dat format 3) stops new images once it
// ends; images, tasks and exports stay usable. The latest time the software was
// seen running is kept with the trial counter (outside the data folder), so
// setting the clock back does not extend the licence: a clock more than an hour
// behind that time counts as expired until it is corrected.

const (
	expiryWarning    = 7 * 24 * time.Hour
	clockRollback    = time.Hour
	lastSeenThrottle = 10 * time.Minute
)

// expiredLocked reports whether the licence has ended (or the clock was set back).
func (s *Service) expiredLocked(now time.Time) (bool, string) {
	p := s.lic.profile
	if p == nil || p.ExpiresAt == "" || (s.lic.mode != "trial" && s.lic.mode != "active") {
		return false, ""
	}
	at, e := time.Parse(time.RFC3339, p.ExpiresAt)
	if e != nil {
		return true, s.msg("license.expiry.invalid")
	}
	if seen, e := time.Parse(time.RFC3339, s.lic.shared.LastSeen); e == nil && now.Before(seen.Add(-clockRollback)) {
		return true, s.msg("license.expiry.clockBack")
	}
	if !now.Before(at) {
		return true, s.msg("license.expiry.ended", "date", at.Local().Format("2006-01-02 15:04"))
	}
	return false, ""
}

// noteRunningLocked moves LastSeen forward (written at most every 10 minutes).
func (s *Service) noteRunningLocked(now time.Time) {
	if s.lic.profile == nil || s.lic.profile.ExpiresAt == "" {
		return
	}
	seen, e := time.Parse(time.RFC3339, s.lic.shared.LastSeen)
	if e == nil && now.Sub(seen) < lastSeenThrottle {
		return
	}
	if e == nil && now.Before(seen) {
		return // never move it back
	}
	s.lic.shared.LastSeen = now.UTC().Format(time.RFC3339)
	_ = saveShared(s.lic.profile.SoftwareID, s.lic.shared)
}

// ImportCustomerFile replaces customer.dat beside the program with a renewed one
// from the provider: it must be signed by a trusted key and belong to this
// software (same software ID). The licence is then read again; images, tasks,
// the trial count and the activation stay. A mass-market program in the open
// edition has no software ID yet: importing its first licence file makes it the
// hosted edition.
func (s *Service) ImportCustomerFile(source string) (LicenseStatus, error) {
	data, e := os.ReadFile(source)
	if e != nil {
		return LicenseStatus{}, e
	}
	if len(data) > 1<<20 {
		return LicenseStatus{}, s.fail("license.import.notLicense")
	}
	s.mu.Lock()
	dir := s.programDir
	current := ""
	if s.lic.profile != nil {
		current = s.lic.profile.SoftwareID
	} else if rec, ok := s.licenseRecordLocked(); ok {
		current = rec.SoftwareID
	}
	busy := s.agentRunID != ""
	s.mu.Unlock()
	if dir == "" || licenseKey() == "" {
		return LicenseStatus{}, s.fail("license.import.notUsed")
	}
	if busy {
		return LicenseStatus{}, s.fail("license.import.busy")
	}
	keys := []string{licenseKey()}
	p, e := license.Verify(data, keys)
	if e != nil {
		return LicenseStatus{}, s.fail("license.import.invalid")
	}
	// A customer build that lost its customer.dat on a fresh data folder has no
	// software ID to compare; the provider's signature is the check then.
	if current != "" && p.SoftwareID != current {
		return LicenseStatus{}, s.fail("license.import.other", "id", p.SoftwareID, "current", current)
	}
	target := filepath.Join(dir, license.FileName)
	if old, e := os.ReadFile(target); e == nil {
		_ = atomicWrite(target+".bak", old)
	}
	if e = atomicWrite(target, data); e != nil {
		return LicenseStatus{}, s.fail("license.import.write", "error", e)
	}
	if e = s.LoadLicense(dir); e != nil {
		return LicenseStatus{}, e
	}
	s.mu.Lock()
	msg := s.msg("license.log.imported")
	if p.ExpiresAt != "" {
		if at, e := time.Parse(time.RFC3339, p.ExpiresAt); e == nil {
			msg = s.msg("license.log.importedUntil", "date", at.Local().Format("2006-01-02 15:04"))
		}
	}
	_ = s.store.appendLog("info", "license", "", msg)
	s.mu.Unlock()
	s.signal()
	return s.LicenseStatus(), nil
}
