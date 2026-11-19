package core

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptrace"
	"net/textproto"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"imageworkbench/internal/i18n"
)

type Provider struct{ Client *http.Client }

func NewProvider() *Provider {
	transport := newNetworkTransport()
	return &Provider{&http.Client{Transport: transport, Timeout: 10 * time.Minute, CheckRedirect: func(r *http.Request, via []*http.Request) error { return http.ErrUseLastResponse }}}
}
func NormalizeURL(raw, lang string) (string, error) {
	u, e := url.Parse(strings.TrimSpace(raw))
	if e != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return "", errors.New(i18n.T(lang, "connection.url.invalid"))
	}
	if u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", errors.New(i18n.T(lang, "connection.url.extras"))
	}
	return strings.TrimRight(u.String(), "/"), nil
}
// redactIn hides key in text with the hidden marker in lang and caps the length.
func redactIn(lang, text, key string) string {
	if key != "" {
		text = strings.ReplaceAll(text, key, i18n.T(lang, "log.hidden"))
	}
	if len(text) > 600 {
		text = text[:600]
	}
	return text
}
func (p *Provider) Models(ctx context.Context, c Connection, key string) ([]string, error) {
	req, e := http.NewRequestWithContext(ctx, "GET", c.BaseURL+"/models", nil)
	if e != nil {
		return nil, e
	}
	setProviderAuth(req, c, key)
	resp, e := p.Client.Do(req)
	if e != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, failCtx(ctx, "connection.models.timeout")
		}
		if errors.Is(ctx.Err(), context.Canceled) {
			return nil, failCtx(ctx, "connection.test.canceled")
		}
		return nil, failCtx(ctx, "connection.models.failed", "error", redactCtx(ctx, e.Error(), key))
	}
	defer resp.Body.Close()
	reader := &responseReader{Reader: io.LimitReader(resp.Body, (4<<20)+1)}
	var b json.RawMessage
	e = json.NewDecoder(reader).Decode(&b)
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d：%s", resp.StatusCode, apiMessage(b, key, i18n.FromContext(ctx)))
	}
	if e != nil || reader.received > 4<<20 {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, failCtx(ctx, "connection.models.timeoutPartial")
		}
		if errors.Is(ctx.Err(), context.Canceled) {
			return nil, failCtx(ctx, "connection.test.canceled")
		}
		return nil, failCtx(ctx, "connection.models.incomplete")
	}
	var result struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if e = json.Unmarshal(b, &result); e != nil {
		return nil, failCtx(ctx, "connection.models.notJSON")
	}
	ids := []string{}
	for _, m := range result.Data {
		if m.ID != "" {
			ids = append(ids, m.ID)
		}
	}
	return ids, nil
}

// referenceImageType returns the sniffed image MIME type of an uploaded reference.
func referenceImageType(data []byte) string {
	if t := http.DetectContentType(data); strings.HasPrefix(t, "image/") {
		return t
	}
	return "application/octet-stream"
}
func apiMessage(b []byte, key, lang string) string {
	var r struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
		Message string `json:"message"`
	}
	if json.Unmarshal(b, &r) == nil {
		if r.Error.Message != "" {
			return redactIn(lang, r.Error.Message, key)
		}
		if r.Message != "" {
			return redactIn(lang, r.Message, key)
		}
	}
	return i18n.T(lang, "connection.refused")
}
func (p *Provider) Generate(ctx context.Context, t Task, key string, refs []Asset, root string) ([]Output, bool, error) {
	return p.GenerateWithProgress(ctx, t, key, refs, root, nil)
}
func (p *Provider) GenerateWithProgress(ctx context.Context, t Task, key string, refs []Asset, root string, progress func(GenerationDiagnostic)) ([]Output, bool, error) {
	start := time.Now()
	var guard sync.Mutex
	finished := false
	endpoint := "/images/generations"
	if len(refs) > 0 {
		endpoint = "/images/edits"
	}
	d := GenerationDiagnostic{Stage: "preparing", StartedAt: Now(), RequestURL: t.Connection.BaseURL + endpoint}
	report := func(update func(*GenerationDiagnostic)) {
		guard.Lock()
		defer guard.Unlock()
		if finished {
			return
		}
		update(&d)
		d.ElapsedMS = time.Since(start).Milliseconds()
		if progress != nil {
			progress(d)
		}
	}
	defer func() {
		guard.Lock()
		finished = true
		d.ElapsedMS = time.Since(start).Milliseconds()
		if progress != nil {
			progress(d)
		}
		guard.Unlock()
	}()
	trace := &httptrace.ClientTrace{GotConn: func(httptrace.GotConnInfo) { report(func(d *GenerationDiagnostic) { d.Stage = "connected" }) }, WroteRequest: func(info httptrace.WroteRequestInfo) {
		if info.Err == nil {
			report(func(d *GenerationDiagnostic) { d.Stage = "waiting" })
		}
	}, GotFirstResponseByte: func() { report(func(d *GenerationDiagnostic) { d.Stage = "receiving" }) }}
	ctx = httptrace.WithClientTrace(ctx, trace)
	return p.generate(ctx, t, key, refs, root, report)
}
func (p *Provider) generate(ctx context.Context, t Task, key string, refs []Asset, root string, report func(func(*GenerationDiagnostic))) ([]Output, bool, error) {
	if imageProtocol(t.Connection) == protocolAsyncJobs {
		return p.generateAsync(ctx, t, key, refs, root, report)
	}
	req, e := buildImageRequest(ctx, t, key, refs, root, false)
	if e != nil {
		return nil, false, e
	}
	if transport, ok := p.Client.Transport.(*http.Transport); ok {
		proxied := false
		if transport.Proxy != nil {
			u, err := transport.Proxy(req)
			proxied = err == nil && u != nil
		}
		report(func(d *GenerationDiagnostic) {
			d.NetworkRoute = "direct"
			if proxied {
				d.NetworkRoute = "proxy"
			}
		})
	}
	// No automatic POST retries or assumed provider idempotency.
	report(func(d *GenerationDiagnostic) { d.Stage = "connecting" })
	// The request is only written after a connection exists (GotConn). A failure
	// before that, such as a proxy that hangs up, sent nothing to the image service.
	var connected atomic.Bool
	req = req.WithContext(httptrace.WithClientTrace(req.Context(), &httptrace.ClientTrace{GotConn: func(httptrace.GotConnInfo) { connected.Store(true) }}))
	resp, e := p.Client.Do(req)
	if e != nil {
		detail := redactCtx(ctx, e.Error(), key)
		report(func(d *GenerationDiagnostic) { d.NetworkError = detail })
		if !connected.Load() {
			return nil, false, failCtx(ctx, "generation.notConnected", "error", detail)
		}
		return nil, true, failCtx(ctx, "generation.sentNoResult", "problem", networkProblem(ctx, e, "generation.noResultAfterSubmit"), "error", detail)
	}
	defer resp.Body.Close()
	report(func(d *GenerationDiagnostic) {
		d.HTTPStatus = resp.StatusCode
		d.ContentType = redactCtx(ctx, resp.Header.Get("Content-Type"), key)
		d.UpstreamRequestID = redactCtx(ctx, resp.Header.Get("X-Request-Id"), key)
		d.ContentLength = resp.ContentLength
		d.TransferEncoding = strings.Join(resp.TransferEncoding, ", ")
		d.HTTPProtocol = resp.Proto
	})
	// Decode the first complete JSON value. A relay can flush valid JSON but
	// keep the HTTP stream open; waiting for EOF would hide a finished result.
	reader := &responseReader{Reader: io.LimitReader(resp.Body, (96<<20)+1), report: report}
	var b json.RawMessage
	e = json.NewDecoder(reader).Decode(&b)
	report(func(d *GenerationDiagnostic) { d.BytesReceived = reader.received })
	if reader.received > 96<<20 {
		return nil, true, failCtx(ctx, "generation.responseTooLarge")
	}
	if e != nil {
		detail := redactCtx(ctx, e.Error(), key)
		report(func(d *GenerationDiagnostic) { d.NetworkError = detail })
		return nil, true, failCtx(ctx, "generation.partialResponse", "problem", networkProblem(ctx, e, "generation.incompleteJSON"), "status", resp.StatusCode, "bytes", reader.received, "error", detail)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, resp.StatusCode >= 500 || resp.StatusCode == 408, fmt.Errorf("HTTP %d：%s", resp.StatusCode, apiMessage(b, key, i18n.FromContext(ctx)))
	}
	var r struct {
		Data []Output `json:"data"`
	}
	if json.Unmarshal(b, &r) != nil || len(r.Data) == 0 {
		return nil, true, failCtx(ctx, "generation.noImageInReply")
	}
	for _, o := range r.Data {
		if o.URL == "" && o.Base64 == "" {
			return nil, true, failCtx(ctx, "generation.unsupportedResult")
		}
	}
	return r.Data, false, nil
}

type responseReader struct {
	io.Reader
	received   int
	lastReport time.Time
	report     func(func(*GenerationDiagnostic))
}

func (r *responseReader) Read(b []byte) (int, error) {
	n, e := r.Reader.Read(b)
	r.received += n
	if r.report != nil && n > 0 && time.Since(r.lastReport) >= 500*time.Millisecond {
		r.report(func(d *GenerationDiagnostic) { d.BytesReceived = r.received })
		r.lastReport = time.Now()
	}
	return n, e
}
// networkProblem names what went wrong with a request, in the language of ctx;
// fallback is the message key used when it was neither a timeout nor an abort.
func networkProblem(ctx context.Context, e error, fallback string) string {
	var n net.Error
	if errors.Is(e, context.DeadlineExceeded) || (errors.As(e, &n) && n.Timeout()) {
		return msgCtx(ctx, "generation.waitTimeout")
	}
	if errors.Is(e, context.Canceled) {
		return msgCtx(ctx, "generation.abortedLocally")
	}
	return msgCtx(ctx, fallback)
}
func validOutputs(outputs []Output) bool {
	if len(outputs) == 0 {
		return false
	}
	for _, o := range outputs {
		if o.URL == "" && o.Base64 == "" {
			return false
		}
	}
	return true
}

// buildImageRequest is the generation or edit request of t (multipart with
// reference images); async marks it for an async-jobs connection.
func buildImageRequest(ctx context.Context, t Task, key string, refs []Asset, root string, async bool) (*http.Request, error) {
	var body io.Reader
	contentType := "application/json"
	endpoint := "/images/generations"
	if len(refs) > 0 {
		endpoint = "/images/edits"
		var b bytes.Buffer
		w := multipart.NewWriter(&b)
		fields := map[string]string{"model": t.Model, "prompt": t.Prompt, "n": "1"}
		if async {
			fields["async"] = "true"
		}
		for k, v := range fields {
			if e := w.WriteField(k, v); e != nil {
				return nil, e
			}
		}
		for k, v := range map[string]string{"size": t.Size, "quality": t.Quality} {
			if v != "" && v != "auto" {
				if e := w.WriteField(k, v); e != nil {
					return nil, e
				}
			}
		}
		field := "image"
		if len(refs) > 1 {
			field = "image[]"
		}
		for _, a := range refs {
			data, e := readOwned(root, a.Path)
			if e != nil {
				return nil, e
			}
			// OpenAI-compatible edits reject application/octet-stream (CreateFormFile's
			// default), so each part declares the detected image type.
			header := textproto.MIMEHeader{}
			header.Set("Content-Disposition", multipart.FileContentDisposition(field, filepath.Base(a.Path)))
			header.Set("Content-Type", referenceImageType(data))
			part, e := w.CreatePart(header)
			if e != nil {
				return nil, e
			}
			if _, e = part.Write(data); e != nil {
				return nil, e
			}
		}
		if e := w.Close(); e != nil {
			return nil, e
		}
		body = &b
		contentType = w.FormDataContentType()
	} else {
		payload := map[string]any{"model": t.Model, "prompt": t.Prompt, "n": 1}
		if async {
			payload["async"] = true
		}
		if t.Size != "" && t.Size != "auto" {
			payload["size"] = t.Size
		}
		if t.Quality != "" && t.Quality != "auto" {
			payload["quality"] = t.Quality
		}
		b, e := json.Marshal(payload)
		if e != nil {
			return nil, e
		}
		body = bytes.NewReader(b)
	}
	req, e := http.NewRequestWithContext(ctx, "POST", t.Connection.BaseURL+endpoint, body)
	if e != nil {
		return nil, e
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("X-Client-Request-Id", t.ID)
	return req, nil
}

// redactCtx is redactIn in the language carried by ctx.
func redactCtx(ctx context.Context, text, key string) string {
	return redactIn(i18n.FromContext(ctx), text, key)
}

// redact is redactIn in the current interface language.
func (s *Service) redact(text, key string) string { return redactIn(s.Language(), text, key) }
