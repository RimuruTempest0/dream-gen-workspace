package core

// The queue: claiming waiting tasks, submitting them once and downloading results.

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// loop dispatches queued tasks oldest first, keeping up to the configured number
// in flight. Each task is still submitted once; parallelism never adds retries.
func (s *Service) loop() {
	defer close(s.done)
	// Close waits for in-flight requests before the store is closed.
	defer s.workers.Wait()
	for {
		if s.ctx.Err() != nil {
			return
		}
		started := false
		for {
			downloading := s.claimTask(true)
			generating := s.claimTask(false)
			if !downloading && !generating {
				break
			}
			started = true
		}
		if !started {
			select {
			case <-s.ctx.Done():
				return
			case <-s.wake:
			case <-time.After(time.Second):
			}
		}
	}
}

// claimTask starts one waiting task in its own goroutine when its pool has room.
// Generation requests are limited by the user's setting; downloads of results
// already returned use a separate pool, so a slow image host never holds a
// generation slot.
func (s *Service) claimTask(download bool) bool {
	s.mu.Lock()
	pool, limit, status := s.inflight, s.imageConcurrency, "queued"
	if download {
		pool, limit, status = s.downloads, maxParallelDownloads, "downloading"
	}
	if s.paused || s.ctx.Err() != nil || len(pool) >= limit {
		s.mu.Unlock()
		return false
	}
	// Only unfinished and recent tasks: an idle pass does not read the history.
	tasks, e := workingTasks(s.store, healthWindow)
	if e != nil {
		s.mu.Unlock()
		return false
	}
	var chosen *Task
	var hc *healthCache
	// A sample check left unfinished when the app last exited is resumed.
	if !download {
		for _, x := range tasks {
			if x.Sample && x.Status == "succeeded" && x.SampleCheck == "checking" {
				s.startSampleCheckLocked(x.ID)
			}
		}
	}
	var g GatewaySettings
	if !download {
		s.maybeProbeLocked(tasks)
		hc = &healthCache{s: s, tasks: tasks, now: time.Now(), seen: map[string]GatewayHealth{}}
		g = s.gatewaySettingsLocked()
	}
	for i := len(tasks) - 1; i >= 0; i-- {
		if tasks[i].Status == status && !s.inflight[tasks[i].ID] && !s.downloads[tasks[i].ID] {
			t := tasks[i]
			if download && waitingToDownload(t, time.Now()) {
				continue
			}
			// A refused image waits (unsent) while the LLM rewrites its prompt; one
			// left waiting when the app last exited is rewritten now.
			if !download && t.Rewording {
				s.startRewordLocked(t.ID)
				continue
			}
			// An image drawn from the suite's detail sample waits until the sample
			// is done; nothing is sent (or billed) while it waits.
			if !download && t.SamplePending {
				if !attachSample(&t, tasks) {
					continue
				}
				_ = s.store.Put("task", t.ID, t)
			}
			// Past the trial limit (or with an invalid licence) nothing more is sent.
			if !download && s.trialStopLocked() {
				t.Status, t.UpdatedAt = "blocked", Now()
				t.Error = "此图尚未提交、未计费。" + trialUsedUp
				if msg := s.licenseBlockMessageLocked(); msg != "" {
					t.Error = "此图尚未提交、未计费。" + msg
				}
				_ = s.store.Put("task", t.ID, t)
				continue
			}
			// An image is sent only to a gateway that is not known to be failing;
			// it may move to a healthy backup first (it was never sent, no cost).
			if !download && !s.routeLocked(&t, g, hc) {
				continue
			}
			chosen = &t
			break
		}
	}
	if chosen == nil {
		s.mu.Unlock()
		return false
	}
	pool[chosen.ID] = true
	if !download {
		s.genGateway[chosen.ID] = chosen.Connection.ID
	}
	s.workers.Add(1)
	s.mu.Unlock()
	go func(t Task) {
		defer func() {
			s.mu.Lock()
			delete(pool, t.ID)
			delete(s.genGateway, t.ID)
			s.mu.Unlock()
			s.workers.Done()
			s.signal()
		}()
		s.step(t)
	}(*chosen)
	return true
}

// step runs one claimed task: submit once, then download its result.
func (s *Service) step(claimed Task) bool {
	s.mu.Lock()
	// Re-read under the lock: the task may have been stopped since it was claimed.
	var t Task
	if s.store.Get("task", claimed.ID, &t) != nil || (t.Status != "queued" && t.Status != "downloading") || t.Rewording || s.paused {
		s.mu.Unlock()
		return false
	}
	if t.Status == "downloading" {
		s.mu.Unlock()
		s.download(&t)
		return true
	}
	key, e := s.key(t.Connection.Credential)
	if e != nil {
		t.Status = "blocked"
		t.Error = e.Error()
		s.store.Put("task", t.ID, t)
		s.mu.Unlock()
		s.signal()
		return true
	}
	refs := []Asset{}
	for _, id := range t.References {
		var a Asset
		if e = s.store.Get("asset", id, &a); e != nil {
			break
		}
		refs = append(refs, a)
	}
	if e != nil {
		t.Status = "failed"
		t.Error = "参考图记录不可用"
		s.store.Put("task", t.ID, t)
		s.mu.Unlock()
		s.signal()
		return true
	}
	t.Status = "submitting"
	t.UpdatedAt = Now()
	if e = s.store.Put("task", t.ID, t); e != nil {
		s.mu.Unlock()
		return false
	}
	ctx, cancel := context.WithCancel(s.ctx)
	defer cancel()
	s.genCancel[t.ID] = cancel
	s.mu.Unlock()
	s.signal()
	outputs, unknown, e := s.provider.GenerateWithProgress(ctx, t, key, refs, s.store.Root, func(d GenerationDiagnostic) {
		s.mu.Lock()
		t.Diagnostic = d
		s.store.Put("task", t.ID, t)
		s.mu.Unlock()
		s.signal()
	})
	s.mu.Lock()
	delete(s.genCancel, t.ID)
	userCanceled := s.canceled[t.ID]
	delete(s.canceled, t.ID)
	// A result that arrived anyway is kept (it may be billed); otherwise the
	// cancellation is recorded instead of a failure.
	if userCanceled && e != nil {
		t.Status, t.UpdatedAt = "canceled", Now()
		t.Error = canceledInFlight
		saveErr := s.store.Put("task", t.ID, t)
		_ = s.store.appendLog("warn", "image", t.ID, fmt.Sprintf("已取消生成：请求发出后等待 %d 秒时取消", t.Diagnostic.ElapsedMS/1000))
		s.mu.Unlock()
		s.signal()
		return saveErr == nil
	}
	// A rejected key or an empty account fails every remaining image the same way.
	// With a gateway setup, the gateway is marked down and unsent images move to a
	// backup or pause there; without one, the rest of the batch pauses unsent.
	// A refusal by the image model's content check concerns this image only: it
	// is sent again automatically and never marks the gateway as failing.
	rejected := ""
	policy := e != nil && !unknown && policyRejected(t.Diagnostic.HTTPStatus, e.Error())
	if e != nil && !unknown && !policy {
		rejected = rejectionKind(t.Diagnostic.HTTPStatus, e.Error())
	}
	if e != nil {
		t.Status = "failed"
		if unknown {
			t.Status = "unknown"
		}
		t.Error = redact(e.Error(), key)
		switch {
		case policy:
			t.Error = fmt.Sprintf("%s（HTTP %d），图片没有生成，一般不计费。%s原始错误：%s", policyRefusedPrefix, t.Diagnostic.HTTPStatus, s.policyRetryLocked(&t), t.Error)
		case rejected == "balance" && t.Trial:
			// The trial key's upstream quota is the hard trial limit.
			t.Error = fmt.Sprintf("试用额度已用完（HTTP %d，图片服务返回额度不足），此图未生成也不计费。请在“系统 → 连接”输入激活码后继续使用。原始错误：%s", t.Diagnostic.HTTPStatus, t.Error)
		case rejected == "balance":
			t.Error = fmt.Sprintf("图片服务账户余额不足（HTTP %d），图片服务拒绝了请求，未生成也不计费。请到该图片服务后台充值后重新生成，API Key 不需要重新输入。原始错误：%s", t.Diagnostic.HTTPStatus, t.Error)
		case rejected == "auth":
			t.Error = fmt.Sprintf("API Key 无效（HTTP %d），图片服务拒绝了请求，未生成也不计费。请在“系统 → 连接”重新输入该图片服务的 API Key 后重新生成。原始错误：%s", t.Diagnostic.HTTPStatus, t.Error)
			// Re-check the key right away with the free probe.
			delete(s.probes, t.Connection.ID)
		}
		// A trial key at its quota ends the trial: pause the rest, whatever the routing.
		if rejected != "" && (!s.gatewaySetupLocked() || t.Trial) {
			kind := rejected
			if kind == "balance" && t.Trial {
				kind = "trial"
			}
			s.pauseBatchForRejectionLocked(t, kind)
		}
		if unknown {
			note := s.autoRegenerateLocked(&t)
			if connectionLost(e) {
				t.Error = fmt.Sprintf("等待约 %d 秒时发现与图片服务的连接已经断开（网络或代理中断，连接不再响应），没能收到结果。这张图可能已在图片服务生成并计费。%s原始错误：%s", t.Diagnostic.ElapsedMS/1000, note, t.Error)
			} else if relayTimedOut(t.Diagnostic) {
				t.Error = fmt.Sprintf("图片服务等待约 %d 秒后断开（%s），没有返回确定的生图结果。这张图可能仍在图片服务生成并已计费。降低同时生成张数或质量可能减少这类超时，但不能保证避免。%s原始错误：%s", t.Diagnostic.ElapsedMS/1000, timeoutLabel(t.Diagnostic), note, t.Error)
			} else {
				t.Error = "结果未知，这张图可能已生成并计费。" + note + "原始错误：" + t.Error
			}
		}
	} else {
		t.Status = "downloading"
		t.Results = outputs
		t.Error = ""
	}
	t.UpdatedAt = Now()
	saveErr := s.store.Put("task", t.ID, t)
	s.mu.Unlock()
	s.signal()
	if saveErr != nil {
		return false
	}
	// The result is saved as "downloading"; the download pool fetches it, so this
	// generation slot is free for the next image right away.
	return true
}

// relayTimedOut recognises a relay or CDN cutting a long-running request: gateway
// timeout statuses, or a connection closed without a reply after a long wait.
func relayTimedOut(d GenerationDiagnostic) bool {
	switch d.HTTPStatus {
	case 504, 522, 524:
		return true
	case 0:
		return d.ElapsedMS >= 90_000 && d.NetworkError != ""
	}
	return false
}

// connectionLost is a connection closed after it stopped answering pings (see
// http2PingAfter): the tunnel under it died, the service did not cut the request.
func connectionLost(e error) bool {
	return e != nil && strings.Contains(e.Error(), "client connection lost")
}

func timeoutLabel(d GenerationDiagnostic) string {
	if d.HTTPStatus != 0 {
		return fmt.Sprintf("HTTP %d", d.HTTPStatus)
	}
	return "连接被关闭"
}

// pauseBatchForRejectionLocked blocks the batch's queued, unsent images after the
// relay rejected the API key or the account has no balance. Requests already in
// flight finish on their own.
func (s *Service) pauseBatchForRejectionLocked(failed Task, kind string) {
	reason, fix, logText := "API Key 无效", "重新输入密钥后可恢复生成。", "图片服务拒绝 API Key"
	if kind == "balance" {
		reason, fix, logText = "图片服务账户余额不足", "到图片服务充值后可恢复生成。", "图片服务账户余额不足"
	} else if kind == "trial" {
		reason, fix, logText = "试用额度已用完", "输入激活码后可恢复生成。", "试用额度已用完"
	}
	if failed.BatchID == "" {
		return
	}
	tasks, e := List[Task](s.store, "task")
	if e != nil {
		return
	}
	paused := 0
	for _, t := range tasks {
		if t.BatchID == failed.BatchID && t.Status == "queued" && t.Connection.ID == failed.Connection.ID {
			t.Status = "blocked"
			t.Error = "同批图片因" + reason + "已暂停，尚未提交、未计费。" + fix
			t.UpdatedAt = Now()
			if s.store.Put("task", t.ID, t) == nil {
				paused++
			}
		}
	}
	if paused > 0 {
		_ = s.store.appendLog("warn", "image", failed.BatchID, fmt.Sprintf("%s，已暂停同批 %d 张未提交图片", logText, paused))
	}
}

func (s *Service) download(t *Task) {
	s.mu.Lock()
	t.DownloadRetryAt = ""
	t.Diagnostic.DownloadStartedAt = Now()
	t.Diagnostic.DownloadMS = 0
	s.store.Put("task", t.ID, *t)
	s.mu.Unlock()
	s.signal()
	for i, out := range t.Results {
		assetID := fmt.Sprintf("%s-%d", t.ID, i)
		s.mu.Lock()
		var existing Asset
		already := s.store.Get("asset", assetID, &existing) == nil
		s.mu.Unlock()
		if !already {
			b, e := s.outputBytes(s.ctx, out)
			if e != nil {
				s.downloadError(t, e)
				return
			}
			s.mu.Lock()
			_, e = s.saveImage(b, assetID+".png", "generated", t.ID, t.ProjectID, assetID)
			s.mu.Unlock()
			if e != nil {
				s.downloadError(t, e)
				return
			}
		}
		found := false
		for _, id := range t.AssetIDs {
			if id == assetID {
				found = true
			}
		}
		if !found {
			t.AssetIDs = append(t.AssetIDs, assetID)
		}
	}
	s.mu.Lock()
	t.Status = "succeeded"
	t.Error = ""
	t.Results = nil
	t.Diagnostic.DownloadMS = sinceMS(t.Diagnostic.DownloadStartedAt)
	t.UpdatedAt = Now()
	// A detail sample is checked before the images drawn from it start.
	check := s.markSampleForCheckLocked(t)
	s.store.Put("task", t.ID, *t)
	if check {
		s.startSampleCheckLocked(t.ID)
	}
	s.mu.Unlock()
	s.signal()
}

// sinceMS returns milliseconds elapsed since an RFC 3339 timestamp, or 0.
func sinceMS(stamp string) int64 {
	at, e := time.Parse(time.RFC3339Nano, stamp)
	if e != nil {
		return 0
	}
	return time.Since(at).Milliseconds()
}

// autoDownloadDelays are the pauses before each automatic re-download of a result
// that could not be saved. Each one only fetches the returned image again, never
// generates; a variable so tests can shorten it.
var autoDownloadDelays = []time.Duration{30 * time.Second, 2 * time.Minute, 5 * time.Minute}

// autoDownloadWindow is how old a failed download may be to be retried on start;
// result links usually expire after about a day.
const autoDownloadWindow = 24 * time.Hour

// waitingToDownload reports whether an automatic re-download is still pausing.
func waitingToDownload(t Task, now time.Time) bool {
	at, e := time.Parse(time.RFC3339Nano, t.DownloadRetryAt)
	return e == nil && at.After(now)
}

func (s *Service) downloadError(t *Task, e error) {
	s.mu.Lock()
	t.Status = "download_failed"
	t.Error = e.Error()
	t.DownloadRetryAt = ""
	t.Diagnostic.DownloadMS = sinceMS(t.Diagnostic.DownloadStartedAt)
	t.UpdatedAt = Now()
	if t.AutoDownloads < len(autoDownloadDelays) && len(t.Results) > 0 {
		wait := autoDownloadDelays[t.AutoDownloads]
		t.AutoDownloads++
		t.Status = "downloading"
		t.Error = ""
		t.DownloadRetryAt = time.Now().Add(wait).UTC().Format(time.RFC3339Nano)
		_ = s.store.appendLog("warn", "image", t.BatchID, fmt.Sprintf("图片下载未完成，%d 秒后自动补下载（第 %d 次，不会重新生图）：%s", int(wait.Seconds()), t.AutoDownloads, e.Error()))
	}
	s.store.Put("task", t.ID, *t)
	s.mu.Unlock()
	s.signal()
}
