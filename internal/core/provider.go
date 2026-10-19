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
	"time"
)

type Provider struct{ Client *http.Client }

func NewProvider() *Provider {
	transport := newNetworkTransport()
	return &Provider{&http.Client{Transport: transport, Timeout: 10 * time.Minute, CheckRedirect: func(r *http.Request, via []*http.Request) error { return http.ErrUseLastResponse }}}
}
func NormalizeURL(raw string) (string, error) {
	u, e := url.Parse(strings.TrimSpace(raw))
	if e != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return "", errors.New("请输入完整的 HTTP/HTTPS API 地址，例如 https://example.com/v1")
	}
	if u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", errors.New("地址不能包含用户名、密码、查询参数或片段")
	}
	return strings.TrimRight(u.String(), "/"), nil
}
func redact(text, key string) string {
	if key != "" {
		text = strings.ReplaceAll(text, key, "[已隐藏]")
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
			return nil, errors.New("模型查询超时（30 秒），请检查网络、代理和 API 前缀")
		}
		if errors.Is(ctx.Err(), context.Canceled) {
			return nil, errors.New("连接测试已取消")
		}
		return nil, fmt.Errorf("连接失败：%s。请检查地址、网络或证书", redact(e.Error(), key))
	}
	defer resp.Body.Close()
	reader := &responseReader{Reader: io.LimitReader(resp.Body, (4<<20)+1)}
	var b json.RawMessage
	e = json.NewDecoder(reader).Decode(&b)
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d：%s", resp.StatusCode, apiMessage(b, key))
	}
	if e != nil || reader.received > 4<<20 {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, errors.New("模型查询超时（30 秒），尚未收到完整响应")
		}
		if errors.Is(ctx.Err(), context.Canceled) {
			return nil, errors.New("连接测试已取消")
		}
		return nil, errors.New("模型列表不是完整的兼容 JSON 响应，可检查 API 前缀或手动填写模型 ID")
	}
	var result struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if e = json.Unmarshal(b, &result); e != nil {
		return nil, errors.New("模型列表不是兼容的 JSON 响应，可手动填写模型 ID")
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
func apiMessage(b []byte, key string) string {
	var r struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
		Message string `json:"message"`
	}
	if json.Unmarshal(b, &r) == nil {
		if r.Error.Message != "" {
			return redact(r.Error.Message, key)
		}
		if r.Message != "" {
			return redact(r.Message, key)
		}
	}
	return "服务拒绝请求，请检查连接和模型配置"
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
	var body io.Reader
	contentType := "application/json"
	endpoint := "/images/generations"
	if len(refs) > 0 {
		endpoint = "/images/edits"
		var b bytes.Buffer
		w := multipart.NewWriter(&b)
		for k, v := range map[string]string{"model": t.Model, "prompt": t.Prompt, "n": "1"} {
			if e := w.WriteField(k, v); e != nil {
				return nil, false, e
			}
		}
		for k, v := range map[string]string{"size": t.Size, "quality": t.Quality} {
			if v != "" && v != "auto" {
				if e := w.WriteField(k, v); e != nil {
					return nil, false, e
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
				return nil, false, e
			}
			// OpenAI-compatible edits reject application/octet-stream (CreateFormFile's
			// default), so each part declares the detected image type.
			header := textproto.MIMEHeader{}
			header.Set("Content-Disposition", multipart.FileContentDisposition(field, filepath.Base(a.Path)))
			header.Set("Content-Type", referenceImageType(data))
			part, e := w.CreatePart(header)
			if e != nil {
				return nil, false, e
			}
			if _, e = part.Write(data); e != nil {
				return nil, false, e
			}
		}
		if e := w.Close(); e != nil {
			return nil, false, e
		}
		body = &b
		contentType = w.FormDataContentType()
	} else {
		payload := map[string]any{"model": t.Model, "prompt": t.Prompt, "n": 1}
		if t.Size != "" && t.Size != "auto" {
			payload["size"] = t.Size
		}
		if t.Quality != "" && t.Quality != "auto" {
			payload["quality"] = t.Quality
		}
		b, e := json.Marshal(payload)
		if e != nil {
			return nil, false, e
		}
		body = bytes.NewReader(b)
	}
	req, e := http.NewRequestWithContext(ctx, "POST", t.Connection.BaseURL+endpoint, body)
	if e != nil {
		return nil, false, e
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("X-Client-Request-Id", t.ID)
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
	resp, e := p.Client.Do(req)
	if e != nil {
		detail := redact(e.Error(), key)
		report(func(d *GenerationDiagnostic) { d.NetworkError = detail })
		return nil, true, fmt.Errorf("%s（%s）。请先核对平台记录，避免重复扣费", networkProblem(e, "提交后未收到确定结果"), detail)
	}
	defer resp.Body.Close()
	report(func(d *GenerationDiagnostic) {
		d.HTTPStatus = resp.StatusCode
		d.ContentType = redact(resp.Header.Get("Content-Type"), key)
		d.UpstreamRequestID = redact(resp.Header.Get("X-Request-Id"), key)
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
		return nil, true, errors.New("图片响应超过 96 MiB 限制，请核对平台记录")
	}
	if e != nil {
		detail := redact(e.Error(), key)
		report(func(d *GenerationDiagnostic) { d.NetworkError = detail })
		return nil, true, fmt.Errorf("%s（HTTP %d，已接收 %d 字节，%s），结果未知，请核对平台记录", networkProblem(e, "未收到完整 JSON 图片响应"), resp.StatusCode, reader.received, detail)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, resp.StatusCode >= 500 || resp.StatusCode == 408, fmt.Errorf("HTTP %d：%s", resp.StatusCode, apiMessage(b, key))
	}
	var r struct {
		Data []Output `json:"data"`
	}
	if json.Unmarshal(b, &r) != nil || len(r.Data) == 0 {
		return nil, true, errors.New("服务已响应，但没有兼容的图片结果；请核对平台记录")
	}
	for _, o := range r.Data {
		if o.URL == "" && o.Base64 == "" {
			return nil, true, errors.New("图片结果格式不受支持，请核对平台记录")
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
func networkProblem(e error, fallback string) string {
	var n net.Error
	if errors.Is(e, context.DeadlineExceeded) || (errors.As(e, &n) && n.Timeout()) {
		return "等待中转站响应超时"
	}
	if errors.Is(e, context.Canceled) {
		return "请求被本地中止"
	}
	return fallback
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
