package admin

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"imageworkbench/internal/core"
	"imageworkbench/internal/license"
)

// ---- upstreams ---------------------------------------------------------------

func (a *Admin) SaveUpstream(u Upstream) (Upstream, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	base, e := core.NormalizeURL(u.BaseURL)
	if e != nil {
		return u, e
	}
	u.BaseURL, u.Name, u.Model = base, strings.TrimSpace(u.Name), strings.TrimSpace(u.Model)
	if u.Name == "" || u.Model == "" || (u.Kind != "image" && u.Kind != "llm") {
		return u, errors.New("请填写上游名称、类型和模型")
	}
	if u.ConsoleURL = strings.TrimSpace(u.ConsoleURL); u.ConsoleURL != "" {
		if !strings.HasPrefix(u.ConsoleURL, "https://") && !strings.HasPrefix(u.ConsoleURL, "http://") {
			return u, errors.New("后台地址应以 https:// 开头")
		}
	}
	// Sub2API answers /v1/usage next to the API (1 Route: another host; edit it).
	if u.UsageURL = strings.TrimSpace(u.UsageURL); u.UsageURL == "" && u.PerKeyQuota {
		u.UsageURL = u.BaseURL + "/usage"
	}
	if u.UsageURL != "" && !strings.HasPrefix(u.UsageURL, "https://") && !strings.HasPrefix(u.UsageURL, "http://") {
		return u, errors.New("额度查询地址应以 https:// 开头")
	}
	if u.Kind == "llm" {
		if u.Mode == "" {
			u.Mode = "compatible"
		}
		if u.AuthMode == "" {
			u.AuthMode = "api-key"
		}
	} else {
		u.Mode, u.AuthMode, u.MaxOutputTokens = "", "", 0
	}
	if u.ID == "" {
		u.ID = newID()
		a.d.Upstreams = append(a.d.Upstreams, u)
	} else {
		old, e := a.upstreamLocked(u.ID)
		if e != nil {
			return u, e
		}
		if old.Kind != u.Kind {
			return u, errors.New("不能修改上游类型，请另建上游")
		}
		// An LLM key is verified against one address, model and protocol.
		if u.Kind == "llm" && (old.BaseURL != u.BaseURL || old.Model != u.Model || old.Mode != u.Mode || old.AuthMode != u.AuthMode) {
			for i := range a.d.Keys {
				if a.d.Keys[i].UpstreamID == u.ID {
					a.d.Keys[i].Verified, a.d.Keys[i].VerifiedAt = false, ""
				}
			}
		}
		*old = u
		a.markChangedForUpstreamLocked(u.ID)
	}
	return u, a.saveLocked()
}

// A changed address or model reaches customers only with a new package.
func (a *Admin) markChangedForUpstreamLocked(upstreamID string) {
	for _, k := range a.d.Keys {
		if k.UpstreamID == upstreamID && k.CustomerID != "" {
			if c, e := a.customerLocked(k.CustomerID); e == nil {
				c.Changed = true
			}
		}
	}
}

// DeleteUpstream removes an upstream together with its keys, as long as none of
// them was ever handed to a customer (those stay as the delivery record).
func (a *Admin) DeleteUpstream(id string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	refs := []string{}
	for _, k := range a.d.Keys {
		if k.UpstreamID != id {
			continue
		}
		if k.CustomerID != "" || k.Delivered || k.DeliveredBefore {
			return errors.New("此上游的 Key 已分配或交付过，不能删除")
		}
		refs = append(refs, k.SecretRef)
	}
	a.d.Keys = slices.DeleteFunc(a.d.Keys, func(k PoolKey) bool { return k.UpstreamID == id })
	a.d.Upstreams = slices.DeleteFunc(a.d.Upstreams, func(u Upstream) bool { return u.ID == id })
	if e := a.saveLocked(); e != nil {
		return e
	}
	for _, ref := range refs {
		a.removeSecret(ref)
	}
	return nil
}

// ---- key pool ----------------------------------------------------------------

type ImportResult struct {
	Added      int `json:"added"`
	Duplicates int `json:"duplicates"`
	Invalid    int `json:"invalid"`
}

// ImportKeys adds one key per line. Keys already in the pool (by hash) are
// skipped, so the same key can never be handed out twice.
func (a *Admin) ImportKeys(upstreamID, use, text, quotaNote string) (ImportResult, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	u, e := a.upstreamLocked(upstreamID)
	if e != nil {
		return ImportResult{}, e
	}
	if use != "primary" && use != "backup" {
		return ImportResult{}, errors.New("请选择用途：主用或备用")
	}
	known := map[string]bool{}
	for _, k := range a.d.Keys {
		known[k.Hash] = true
	}
	var r ImportResult
	for _, line := range strings.Split(text, "\n") {
		key := strings.TrimSpace(line)
		if key == "" {
			continue
		}
		if len(key) < 8 || strings.ContainsAny(key, " \t") {
			r.Invalid++
			continue
		}
		h := keyHash(key)
		if known[h] {
			r.Duplicates++
			continue
		}
		ref, e := a.putSecret([]byte(key))
		if e != nil {
			return r, e
		}
		known[h] = true
		a.d.Keys = append(a.d.Keys, PoolKey{ID: newID(), Hash: h, Tail: key[len(key)-4:], SecretRef: ref, UpstreamID: u.ID, Kind: u.Kind, Use: use, QuotaNote: strings.TrimSpace(quotaNote), Status: "available", ImportedAt: now()})
		r.Added++
	}
	if r.Added == 0 && r.Duplicates == 0 && r.Invalid == 0 {
		return r, errors.New("没有找到 Key，请每行粘贴一个")
	}
	return r, a.saveLocked()
}

func (a *Admin) SetKeyNote(id, note string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	k, e := a.keyLocked(id)
	if e != nil {
		return e
	}
	k.QuotaNote = strings.TrimSpace(note)
	return a.saveLocked()
}

// SetKeyUse switches an unassigned key between primary and backup.
func (a *Admin) SetKeyUse(id, use string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if use != "primary" && use != "backup" {
		return errors.New("用途只能是主用或备用")
	}
	k, e := a.keyLocked(id)
	if e != nil {
		return e
	}
	if k.Status != "available" {
		return errors.New("只能修改未分配的可用 Key")
	}
	k.Use = use
	return a.saveLocked()
}

// DisableKey marks a key unusable (disabled upstream, leaked …). An assigned key
// must be replaced on its customer first.
func (a *Admin) DisableKey(id, reason string) error {
	return a.DisableKeys([]string{id}, reason)
}

// DisableKeys disables several keys at once; nothing changes if any of them is
// still assigned. Keys already disabled keep their reason.
func (a *Admin) DisableKeys(ids []string, reason string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	keys, e := a.keysLocked(ids)
	if e != nil {
		return e
	}
	for _, k := range keys {
		if k.Status == "assigned" {
			return fmt.Errorf("…%s 正分配给客户，请先在客户页撤下或换 Key", k.Tail)
		}
	}
	for _, k := range keys {
		if k.Status != "disabled" {
			k.Status, k.DisabledFor = "disabled", strings.TrimSpace(reason)
		}
	}
	return a.saveLocked()
}

// DeleteKeys removes keys that never left this computer, with their stored
// secrets. Nothing is deleted if any key is assigned or went out in a package:
// those records stop the same key being imported and handed out again.
func (a *Admin) DeleteKeys(ids []string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	keys, e := a.keysLocked(ids)
	if e != nil {
		return e
	}
	drop, refs := map[string]bool{}, []string{}
	for _, k := range keys {
		if k.CustomerID != "" {
			return fmt.Errorf("…%s 正分配给客户，请先在客户页撤下", k.Tail)
		}
		if k.Delivered || k.DeliveredBefore {
			return fmt.Errorf("…%s 交付过，不能删除，只能停用", k.Tail)
		}
		drop[k.ID] = true
		refs = append(refs, k.SecretRef)
	}
	a.d.Keys = slices.DeleteFunc(a.d.Keys, func(k PoolKey) bool { return drop[k.ID] })
	if e := a.saveLocked(); e != nil {
		return e
	}
	for _, ref := range refs {
		a.removeSecret(ref)
	}
	return nil
}

// keysLocked resolves a selection of key IDs, rejecting an empty or unknown one.
func (a *Admin) keysLocked(ids []string) ([]*PoolKey, error) {
	if len(ids) == 0 {
		return nil, errors.New("请先选择 Key")
	}
	out := []*PoolKey{}
	for _, id := range slices.Compact(slices.Sorted(slices.Values(ids))) {
		k, e := a.keyLocked(id)
		if e != nil {
			return nil, e
		}
		out = append(out, k)
	}
	return out, nil
}

// EnableKey puts a disabled key back in the pool. The provider confirms first that
// a key which went out in a package is safe to hand out again; it stays marked as
// delivered before, and a new assignment starts as undelivered.
func (a *Admin) EnableKey(id string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	k, e := a.keyLocked(id)
	if e != nil {
		return e
	}
	if k.Status != "disabled" {
		return errors.New("只能重新启用已停用的 Key")
	}
	k.DeliveredBefore = k.DeliveredBefore || k.Delivered
	k.Status, k.DisabledFor, k.Delivered = "available", "", false
	return a.saveLocked()
}

// ---- customers ---------------------------------------------------------------

func (a *Admin) CreateCustomer(name string, trialImages int) (Customer, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	name = strings.TrimSpace(name)
	if name == "" {
		return Customer{}, errors.New("请填写客户名称")
	}
	for _, c := range a.d.Customers {
		if c.Name == name {
			return Customer{}, errors.New("已有同名客户")
		}
	}
	if trialImages <= 0 {
		trialImages = 20
	}
	id, e := license.NewSoftwareID()
	if e != nil {
		return Customer{}, e
	}
	code, e := license.NewCode()
	if e != nil {
		return Customer{}, e
	}
	ref, e := a.putSecret([]byte(code))
	if e != nil {
		return Customer{}, e
	}
	c := Customer{ID: newID(), Name: name, SoftwareID: id, CodeRef: ref, CreatedAt: now(), TrialImages: trialImages, Slots: map[string]string{}, ImageBackups: slices.Clone(defaultOrder), Deliveries: []Delivery{}, Events: []Event{}, Changed: true}
	c.log("created", "创建客户，软件标识 "+id)
	a.d.Customers = append(a.d.Customers, c)
	return c, a.saveLocked()
}

// RevealCode returns the activation code for showing or resending it.
func (a *Admin) RevealCode(customerID string) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	c, e := a.customerLocked(customerID)
	if e != nil {
		return "", e
	}
	b, e := a.secret(c.CodeRef)
	return string(b), e
}

func (a *Admin) SetCustomerNotes(customerID, plan, quota string, trialImages int) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	c, e := a.customerLocked(customerID)
	if e != nil {
		return e
	}
	c.PlanNote, c.QuotaNote = strings.TrimSpace(plan), strings.TrimSpace(quota)
	if trialImages > 0 && trialImages != c.TrialImages {
		c.TrialImages, c.Changed = trialImages, true
	}
	return a.saveLocked()
}

// AddEvent records renewals and other notes (e.g. "续费 3 个月，额度 ¥500").
func (a *Admin) AddEvent(customerID, kind, text string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	c, e := a.customerLocked(customerID)
	if e != nil {
		return e
	}
	if strings.TrimSpace(text) == "" {
		return errors.New("请填写记录内容")
	}
	if kind != "renewed" {
		kind = "note"
	}
	c.log(kind, strings.TrimSpace(text))
	return a.saveLocked()
}

// ---- assignment --------------------------------------------------------------

// assignLocked checks only the kind: a key's primary/backup use is a pool label,
// so the provider may put any free key of the right kind on any line.
func (a *Admin) assignLocked(c *Customer, slot string, k *PoolKey) error {
	kind, _ := SlotNeeds(slot)
	if k.Kind != kind {
		return fmt.Errorf("%s 需要%s Key", SlotName(slot), kindLabel(kind))
	}
	if k.Status != "available" {
		return errors.New("这个 Key 已分配或已停用，不能重复分配")
	}
	k.Status, k.CustomerID, k.Slot = "assigned", c.ID, slot
	c.Slots[slot] = k.ID
	c.Changed = true
	return nil
}

// releaseLocked takes a key off its slot. A key that went out in a package is
// disabled for good; one never delivered goes back to the pool.
func (a *Admin) releaseLocked(c *Customer, slot, reason string) {
	id := c.Slots[slot]
	delete(c.Slots, slot)
	if k, e := a.keyLocked(id); e == nil {
		k.CustomerID, k.Slot = "", ""
		if k.Delivered {
			k.Status, k.DisabledFor = "disabled", reason
		} else {
			k.Status = "available"
		}
	}
	c.Changed = true
}

// AutoAssign fills every empty slot from available keys of the right kind and
// use; image primaries come from different upstreams where possible.
func (a *Admin) AutoAssign(customerID string) ([]string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	c, e := a.customerLocked(customerID)
	if e != nil {
		return nil, e
	}
	missing := []string{}
	for _, slot := range AllSlots {
		if c.Slots[slot] != "" {
			continue
		}
		kind, use := SlotNeeds(slot)
		taken := map[string]bool{}
		for _, s := range c.Slots {
			if k, e := a.keyLocked(s); e == nil && k.Kind == kind && k.Use == use {
				taken[k.UpstreamID] = true
			}
		}
		var pick *PoolKey
		for i := range a.d.Keys {
			k := &a.d.Keys[i]
			if k.Status != "available" || k.Kind != kind || k.Use != use {
				continue
			}
			if pick == nil || (taken[pick.UpstreamID] && !taken[k.UpstreamID]) {
				pick = k
			}
		}
		if pick == nil {
			missing = append(missing, SlotName(slot))
			continue
		}
		if e = a.assignLocked(c, slot, pick); e != nil {
			return nil, e
		}
		c.log("assigned", fmt.Sprintf("%s 分配 Key …%s", SlotName(slot), pick.Tail))
	}
	return missing, a.saveLocked()
}

// AssignKey puts a chosen key on a slot. If the slot already has a key, that is
// a key replacement (换 Key): the next package carries a new vault revision.
func (a *Admin) AssignKey(customerID, slot, keyID string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	c, e := a.customerLocked(customerID)
	if e != nil {
		return e
	}
	if !slices.Contains(AllSlots, slot) {
		return errors.New("线路不存在")
	}
	k, e := a.keyLocked(keyID)
	if e != nil {
		return e
	}
	old := c.Slots[slot]
	if old == keyID {
		return nil
	}
	if kind, _ := SlotNeeds(slot); k.Kind != kind || k.Status != "available" {
		return a.assignLocked(c, slot, k) // explains why
	}
	if old != "" {
		a.releaseLocked(c, slot, "已为客户“"+c.Name+"”更换")
		c.log("rekeyed", fmt.Sprintf("%s 更换为 Key …%s", SlotName(slot), k.Tail))
	} else {
		c.log("assigned", fmt.Sprintf("%s 分配 Key …%s", SlotName(slot), k.Tail))
	}
	if e = a.assignLocked(c, slot, k); e != nil {
		return e
	}
	return a.saveLocked()
}

func (a *Admin) UnassignKey(customerID, slot string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	c, e := a.customerLocked(customerID)
	if e != nil {
		return e
	}
	if c.Slots[slot] == "" {
		return nil
	}
	a.releaseLocked(c, slot, "已从客户“"+c.Name+"”撤下")
	c.log("assigned", SlotName(slot)+" 撤下 Key")
	return a.saveLocked()
}

// DeleteCustomer removes a customer, its activation code and records. Keys never
// delivered go back to the pool; delivered keys are in the customer's software,
// so they are disabled for good and never handed to anyone else. Package files
// under release\customers are left alone.
func (a *Admin) DeleteCustomer(customerID string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	c, e := a.customerLocked(customerID)
	if e != nil {
		return e
	}
	for _, slot := range AllSlots {
		if c.Slots[slot] != "" {
			a.releaseLocked(c, slot, "客户“"+c.Name+"”已删除")
		}
	}
	ref := c.CodeRef
	a.d.Customers = slices.DeleteFunc(a.d.Customers, func(x Customer) bool { return x.ID == customerID })
	if e = a.saveLocked(); e != nil {
		return e
	}
	a.removeSecret(ref)
	return nil
}

// SetImageBackups sets the order of backup image lines (slots after image-1).
func (a *Admin) SetImageBackups(customerID string, order []string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	c, e := a.customerLocked(customerID)
	if e != nil {
		return e
	}
	want := slices.Clone(defaultOrder)
	got := slices.Clone(order)
	slices.Sort(want)
	slices.Sort(got)
	if !slices.Equal(want, got) {
		return errors.New("备用顺序应包含图片线路 2–6 各一次")
	}
	c.ImageBackups, c.Changed = slices.Clone(order), true
	return a.saveLocked()
}

func kindLabel(kind string) string {
	return map[string]string{"image": "图片", "llm": "LLM"}[kind]
}

// ---- views (never include secrets) -------------------------------------------

type State struct {
	SigningPublicKey string     `json:"signingPublicKey"`
	RepoRoot         string     `json:"repoRoot"`
	DataDir          string     `json:"dataDir"`
	Upstreams        []Upstream `json:"upstreams"`
	Keys             []PoolKey  `json:"keys"`
	Customers        []Customer `json:"customers"`
}

func (a *Admin) State() State {
	a.mu.Lock()
	defer a.mu.Unlock()
	keys := slices.Clone(a.d.Keys)
	for i := range keys {
		keys[i].SecretRef, keys[i].Hash = "", ""
	}
	customers := slices.Clone(a.d.Customers)
	for i := range customers {
		c := &customers[i]
		c.CodeRef = ""
		// Lists go to the UI as [] rather than null.
		if c.Slots == nil {
			c.Slots = map[string]string{}
		}
		if c.ImageBackups == nil {
			c.ImageBackups = slices.Clone(defaultOrder)
		}
		if c.Deliveries == nil {
			c.Deliveries = []Delivery{}
		}
		if c.Events == nil {
			c.Events = []Event{}
		}
	}
	upstreams := slices.Clone(a.d.Upstreams)
	if keys == nil {
		keys = []PoolKey{}
	}
	if customers == nil {
		customers = []Customer{}
	}
	if upstreams == nil {
		upstreams = []Upstream{}
	}
	return State{SigningPublicKey: a.d.SigningPublicKey, RepoRoot: a.d.RepoRoot, DataDir: a.root, Upstreams: upstreams, Keys: keys, Customers: customers}
}

// MarkPaid records that the customer paid and that the provider raised every
// key's quota at the upstream. Only then is the activation code meant to be sent.
func (a *Admin) MarkPaid(customerID, note string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	c, e := a.customerLocked(customerID)
	if e != nil {
		return e
	}
	c.Paid, c.PaidAt = true, now()
	text := "已收款，已在上游后台调高各线路额度"
	if note = strings.TrimSpace(note); note != "" {
		text += "：" + note
	}
	c.log("paid", text)
	return a.saveLocked()
}
