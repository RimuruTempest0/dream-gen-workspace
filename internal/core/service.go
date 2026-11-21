package core

import (
	"context"
	"sync"
	"sync/atomic"
	"time"
)

type Service struct {
	mu                sync.Mutex
	store             *Store
	provider          *Provider
	ctx               context.Context
	cancel            context.CancelFunc
	done              chan struct{}
	wake              chan struct{}
	emit              func()
	paused            bool
	connectionTests   map[string]context.CancelFunc
	agentExecutable   string
	agentCancel       context.CancelFunc
	agentDone         chan struct{}
	agentRunID        string
	agentRunner       func(context.Context, agentInvocation) error
	allowLocalProduct bool
	productBrowser    productBrowserConfig
	browserMu         sync.Mutex
	// imageConcurrency is how many generation tasks run at once (saved preference).
	imageConcurrency int
	inflight         map[string]bool
	downloads        map[string]bool
	// genGateway maps an in-flight generation to its gateway (per-gateway limits).
	genGateway map[string]string
	// genCancel stops the request of an in-flight generation; canceled marks the
	// ones the user cancelled, so the result is recorded as cancelled.
	genCancel map[string]context.CancelFunc
	// replies holds the LLM reply being received for each analyzing run.
	replies  map[string]*liveReply
	canceled map[string]bool
	// probes holds the latest free health check (model list) of each image relay.
	probes  map[string]probeResult
	probing map[string]bool
	// rewording marks held attempts whose prompt the LLM is rewriting.
	rewording map[string]bool
	// sampleChecks marks samples the LLM is checking.
	sampleChecks map[string]bool
	// lic is the customer licence (empty in developer mode).
	lic licenseState
	// programDir is the program folder that holds customer.dat (set by LoadLicense).
	programDir string
	upd        updateState
	// llmCallsSaved counts LLM call records written; old ones are pruned now and then.
	llmCallsSaved int
	workers       sync.WaitGroup
	// lang is the interface language (string); see language.go.
	lang atomic.Value
}

const (
	defaultImageConcurrency = 3
	maxImageConcurrency     = 4
	// Result downloads never call the generation API, so they get their own limit.
	maxParallelDownloads = 4
)

func (s *Service) ImageConcurrency() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.imageConcurrency
}

// SetImageConcurrency changes the number of parallel generation requests. It
// applies immediately: more tasks start when raised; running ones finish when lowered.
func (s *Service) SetImageConcurrency(n int) error {
	if n < 1 || n > maxImageConcurrency {
		return s.fail("task.concurrencyRange", "max", maxImageConcurrency)
	}
	s.mu.Lock()
	if e := s.store.Put("preferences", "image-concurrency", n); e != nil {
		s.mu.Unlock()
		return e
	}
	changed := s.imageConcurrency != n
	s.imageConcurrency = n
	if changed {
		_ = s.store.appendLog("info", "image", "", s.msg("task.log.concurrency", "n", n))
	}
	s.mu.Unlock()
	s.signal()
	return nil
}

func (s *Service) Dispose()     { s.cancel(); s.waitAgent(); s.store.Close() }
func (s *Service) StopForExit() { s.mu.Lock(); s.paused = true; s.mu.Unlock(); s.cancel() }
func (s *Service) StopAllQueued() {
	s.mu.Lock()
	defer s.mu.Unlock()
	tasks, e := listTasksWhere(s.store, taskStatusExpr+`='queued'`)
	if e != nil {
		return
	}
	for _, t := range tasks {
		if t.Status == "queued" {
			pauseTask(&t, s.msg("task.pausedOnExit"))
			s.store.Put("task", t.ID, t)
		}
	}
}
func NewService(root string) (*Service, error) {
	st, e := Open(root)
	if e != nil {
		return nil, e
	}
	lang, e := loadLanguage(st)
	if e != nil {
		st.Close()
		return nil, e
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &Service{store: st, provider: NewProvider(), ctx: ctx, cancel: cancel, done: make(chan struct{}), wake: make(chan struct{}, 1), allowLocalProduct: qaLocalProduct}
	s.lang.Store(lang)
	st.lang = s.Language
	tasks, e := List[Task](st, "task")
	if e != nil {
		st.Close()
		cancel()
		return nil, e
	}
	for _, t := range tasks {
		if t.Status == "queued" && t.AgentRunID != "" {
			var run AgentRun
			if st.Get("agent_run", t.AgentRunID, &run) == nil && run.AutoGeneration {
				pauseTask(&t, s.msg("task.interruptedUnsent"))
				if e = st.Put("task", t.ID, t); e != nil {
					st.Close()
					cancel()
					return nil, e
				}
			}
		}
		// An async-jobs image that was already accepted keeps its job: it is polled
		// again, never submitted (and paid for) a second time.
		if (t.Status == "submitting" || t.Status == "running") && t.Diagnostic.UpstreamJobID != "" {
			t.Status, t.Error, t.UpdatedAt = "queued", "", Now()
			if e = st.Put("task", t.ID, t); e != nil {
				st.Close()
				cancel()
				return nil, e
			}
			_ = st.appendLog("info", "image", t.ID, s.msg("task.log.asyncResumed", "job", t.Diagnostic.UpstreamJobID))
		}
		if t.Status == "submitting" || t.Status == "running" {
			t.Status = "unknown"
			t.Error = s.msg("task.exitedWhileSubmitting")
			if e = st.Put("task", t.ID, t); e != nil {
				st.Close()
				cancel()
				return nil, e
			}
		}
		// A recent result that could not be saved is fetched again automatically
		// (a free GET, never a new generation).
		if updated, err := time.Parse(time.RFC3339Nano, t.UpdatedAt); t.Status == "download_failed" && len(t.Results) > 0 && err == nil && time.Since(updated) < autoDownloadWindow {
			t.Status, t.Error, t.AutoDownloads, t.DownloadRetryAt = "downloading", "", 0, ""
			if e = st.Put("task", t.ID, t); e != nil {
				st.Close()
				cancel()
				return nil, e
			}
		}
	}
	s.inflight = map[string]bool{}
	s.downloads = map[string]bool{}
	s.genGateway = map[string]string{}
	s.genCancel = map[string]context.CancelFunc{}
	s.canceled = map[string]bool{}
	s.probes = map[string]probeResult{}
	s.probing = map[string]bool{}
	s.rewording = map[string]bool{}
	s.sampleChecks = map[string]bool{}
	s.imageConcurrency = defaultImageConcurrency
	var saved int
	if st.Get("preferences", "image-concurrency", &saved) == nil && saved >= 1 {
		// Versions before 0.5.0 allowed up to 8; keep such a choice at the new maximum.
		s.imageConcurrency = min(saved, maxImageConcurrency)
	}
	if e = s.initializeAgents(); e != nil {
		st.Close()
		cancel()
		return nil, e
	}
	_ = st.appendLog("info", "app", "", s.msg("log.started"))
	return s, nil
}
func (s *Service) Start(emit func()) { s.emit = emit; go s.loop(); go s.watchUpdates() }
func (s *Service) Close() {
	s.cancel()
	s.waitAgent()
	<-s.done
	s.mu.Lock()
	defer s.mu.Unlock()
	s.store.Close()
}
func (s *Service) signal() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
	if s.emit != nil {
		s.emit()
	}
}
func (s *Service) key(ref string) (string, error) {
	if ref == "" {
		return "", s.fail("connection.key.none")
	}
	b, e := readOwned(s.store.Root, "secrets/"+ref+".bin")
	if e != nil {
		return "", s.fail("connection.key.unavailable")
	}
	plain, e := Unprotect(b)
	if e != nil {
		return "", s.fail("connection.key.undecryptable")
	}
	return string(plain), nil
}
func (s *Service) Root() string { s.mu.Lock(); defer s.mu.Unlock(); return s.store.Root }
