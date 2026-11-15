package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Image protocols. Each image connection uses one adapter:
//
//   - openai-images (default): one synchronous request to /images/generations or
//     /images/edits returns the image (Provider.generate).
//   - async-jobs: the same request with "async" set and an Idempotency-Key (the
//     task ID) returns a job ID; the app then polls GET /images/jobs/{id} until it
//     finishes. The job ID is saved on the task, so an app restart keeps polling
//     the same job instead of submitting (and paying for) the image again.
//
// The async shape is generic, not any vendor's private API: see
// docs/upstream-capabilities.md. A vendor with its own paths needs its own
// adapter, written against its published documentation.
const (
	protocolOpenAIImages = "openai-images"
	protocolAsyncJobs    = "async-jobs"
)

var imageProtocols = map[string]string{protocolOpenAIImages: "OpenAI Images（同步）", protocolAsyncJobs: "异步任务（提交后轮询）"}

func imageProtocol(c Connection) string {
	if c.Protocol == protocolAsyncJobs {
		return protocolAsyncJobs
	}
	return protocolOpenAIImages
}

// Polling settings; package variables so tests can shorten them.
var (
	asyncPollEvery   = 3 * time.Second
	asyncPollTimeout = 15 * time.Minute
)

// asyncJob is the submit and poll answer of an async-jobs connection. Finished
// answers carry data like the synchronous API.
type asyncJob struct {
	ID     string   `json:"id"`
	TaskID string   `json:"task_id"`
	JobID  string   `json:"job_id"`
	Status string   `json:"status"`
	Data   []Output `json:"data"`
	Error  struct {
		Message string `json:"message"`
	} `json:"error"`
}

func (j asyncJob) id() string {
	for _, v := range []string{j.ID, j.JobID, j.TaskID} {
		if v != "" {
			return v
		}
	}
	return ""
}

// asyncState sorts a job status into running, done or failed.
func asyncState(status string) string {
	switch strings.ToLower(status) {
	case "succeeded", "success", "completed", "complete", "done", "finished":
		return "done"
	case "failed", "failure", "error", "cancelled", "canceled", "rejected", "expired":
		return "failed"
	}
	return "running"
}

// generateAsync submits (unless the task already has a job) and polls the job.
// Like the synchronous call it returns unknown=true when the image may have been
// produced and billed without the app knowing the result.
func (p *Provider) generateAsync(ctx context.Context, t Task, key string, refs []Asset, root string, report func(func(*GenerationDiagnostic))) ([]Output, bool, error) {
	jobID := t.Diagnostic.UpstreamJobID
	if jobID == "" {
		job, unknown, e := p.submitAsync(ctx, t, key, refs, root, report)
		if e != nil {
			return nil, unknown, e
		}
		if asyncState(job.Status) == "done" || (job.id() == "" && validOutputs(job.Data)) {
			if !validOutputs(job.Data) {
				return nil, true, errors.New("异步任务已完成，但没有兼容的图片结果；请核对平台记录")
			}
			return job.Data, false, nil
		}
		if jobID = job.id(); jobID == "" {
			return nil, true, errors.New("图片服务没有返回任务 ID，结果未知，请核对平台记录")
		}
		report(func(d *GenerationDiagnostic) { d.UpstreamJobID = jobID })
	}
	report(func(d *GenerationDiagnostic) { d.Stage = "polling"; d.UpstreamJobID = jobID })
	return p.pollAsync(ctx, t.Connection.BaseURL, jobID, key, report)
}

func (p *Provider) submitAsync(ctx context.Context, t Task, key string, refs []Asset, root string, report func(func(*GenerationDiagnostic))) (asyncJob, bool, error) {
	req, e := buildImageRequest(ctx, t, key, refs, root, true)
	if e != nil {
		return asyncJob{}, false, e
	}
	req.Header.Set("Idempotency-Key", t.ID)
	req.Header.Set("Prefer", "respond-async")
	report(func(d *GenerationDiagnostic) { d.Stage = "connecting" })
	resp, e := p.Client.Do(req)
	if e != nil {
		detail := redact(e.Error(), key)
		report(func(d *GenerationDiagnostic) { d.NetworkError = detail })
		return asyncJob{}, true, fmt.Errorf("%s（%s）。请先核对平台记录，避免重复扣费", networkProblem(e, "提交后未收到确定结果"), detail)
	}
	defer resp.Body.Close()
	report(func(d *GenerationDiagnostic) {
		d.HTTPStatus = resp.StatusCode
		d.UpstreamRequestID = redact(resp.Header.Get("X-Request-Id"), key)
		d.HTTPProtocol = resp.Proto
	})
	b, e := io.ReadAll(io.LimitReader(resp.Body, 96<<20))
	if e != nil {
		return asyncJob{}, true, fmt.Errorf("%s，结果未知，请核对平台记录", networkProblem(e, "未收到完整的提交响应"))
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return asyncJob{}, resp.StatusCode >= 500 || resp.StatusCode == 408, fmt.Errorf("HTTP %d：%s", resp.StatusCode, apiMessage(b, key))
	}
	var job asyncJob
	if json.Unmarshal(b, &job) != nil {
		return asyncJob{}, true, errors.New("提交响应不是兼容的 JSON，结果未知，请核对平台记录")
	}
	return job, false, nil
}

// pollAsync asks for the job until it finishes. Single failed polls (network,
// 5xx) are retried; the image is only reported unknown at the overall deadline.
func (p *Provider) pollAsync(ctx context.Context, base, jobID, key string, report func(func(*GenerationDiagnostic))) ([]Output, bool, error) {
	deadline := time.Now().Add(asyncPollTimeout)
	address := base + "/images/jobs/" + url.PathEscape(jobID)
	lastProblem := ""
	for {
		job, problem, final := p.pollOnce(ctx, address, key)
		if final != nil {
			return nil, false, final
		}
		if problem == "" {
			switch asyncState(job.Status) {
			case "done":
				if !validOutputs(job.Data) {
					return nil, true, errors.New("异步任务已完成，但没有兼容的图片结果；请核对平台记录")
				}
				return job.Data, false, nil
			case "failed":
				msg := strings.TrimSpace(job.Error.Message)
				if msg == "" {
					msg = "图片服务报告任务失败（" + job.Status + "）"
				}
				return nil, false, errors.New(redact(msg, key))
			}
		} else {
			lastProblem = problem
			report(func(d *GenerationDiagnostic) { d.NetworkError = problem })
		}
		if time.Now().After(deadline) {
			msg := fmt.Sprintf("等待异步任务 %s 超过 %d 分钟仍未完成", jobID, int(asyncPollTimeout/time.Minute))
			if lastProblem != "" {
				msg += "（最后一次查询：" + lastProblem + "）"
			}
			return nil, true, errors.New(msg + "，结果未知，请核对平台记录")
		}
		select {
		case <-ctx.Done():
			return nil, true, fmt.Errorf("%s，异步任务 %s 可能仍在生成", networkProblem(ctx.Err(), "请求被本地中止"), jobID)
		case <-time.After(asyncPollEvery):
		}
	}
}

// pollOnce returns the job, or a transient problem to retry, or a final error.
func (p *Provider) pollOnce(ctx context.Context, address, key string) (asyncJob, string, error) {
	req, e := http.NewRequestWithContext(ctx, "GET", address, nil)
	if e != nil {
		return asyncJob{}, "", e
	}
	req.Header.Set("Authorization", "Bearer "+key)
	resp, e := p.Client.Do(req)
	if e != nil {
		return asyncJob{}, redact(e.Error(), key), nil
	}
	defer resp.Body.Close()
	b, e := io.ReadAll(io.LimitReader(resp.Body, 96<<20))
	if e != nil {
		return asyncJob{}, redact(e.Error(), key), nil
	}
	switch {
	case resp.StatusCode >= 500 || resp.StatusCode == 408 || resp.StatusCode == 429:
		return asyncJob{}, fmt.Sprintf("HTTP %d", resp.StatusCode), nil
	case resp.StatusCode == 404:
		return asyncJob{}, "", errors.New("图片服务找不到这个异步任务（HTTP 404），请核对平台记录")
	case resp.StatusCode < 200 || resp.StatusCode >= 300:
		return asyncJob{}, "", fmt.Errorf("查询异步任务失败：HTTP %d：%s", resp.StatusCode, apiMessage(b, key))
	}
	var job asyncJob
	if json.Unmarshal(b, &job) != nil {
		return asyncJob{}, "查询结果不是 JSON", nil
	}
	return job, "", nil
}
