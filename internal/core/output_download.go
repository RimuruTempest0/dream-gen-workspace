package core

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"
)

const imageDownloadTimeout = 10 * time.Minute
const imageDownloadAttempts = 3

func imageDownloadClient() *http.Client {
	return &http.Client{Transport: newNetworkTransport(), CheckRedirect: func(r *http.Request, via []*http.Request) error {
		if len(via) > 5 {
			return failCtx(r.Context(), "data.download.redirects")
		}
		if r.URL.Scheme != "http" && r.URL.Scheme != "https" || r.URL.User != nil {
			return failCtx(r.Context(), "data.download.scheme")
		}
		// Result hosts and redirects must never receive provider credentials.
		r.Header.Del("Authorization")
		return nil
	}}
}

func downloadImage(ctx context.Context, rawURL string) ([]byte, error) {
	client := imageDownloadClient()
	defer client.CloseIdleConnections()
	// One budget covers all GET attempts and backoff, rather than allowing each
	// retry to keep a queue slot occupied for another ten minutes.
	ctx, cancel := context.WithTimeout(ctx, imageDownloadTimeout)
	defer cancel()
	return downloadImageWithClient(ctx, rawURL, client, time.Second)
}

func downloadImageWithClient(ctx context.Context, rawURL string, client *http.Client, retryDelay time.Duration) ([]byte, error) {
	u, e := url.Parse(rawURL)
	if e != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil {
		return nil, failCtx(ctx, "generation.download.badURL")
	}
	for attempt := 1; attempt <= imageDownloadAttempts; attempt++ {
		data, retryable, err := downloadImageAttempt(ctx, rawURL, client)
		if err == nil {
			return data, nil
		}
		if ctx.Err() != nil {
			return nil, imageDownloadContextError(ctx, ctx.Err())
		}
		if !retryable || attempt == imageDownloadAttempts {
			return nil, fmt.Errorf("%w%s", err, msgCtx(ctx, "generation.download.attempts", "n", attempt))
		}
		timer := time.NewTimer(retryDelay * time.Duration(attempt))
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, imageDownloadContextError(ctx, ctx.Err())
		case <-timer.C:
		}
	}
	panic("unreachable")
}

func imageDownloadContextError(ctx context.Context, err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return failCtx(ctx, "generation.download.timeout")
	}
	return failCtx(ctx, "generation.download.aborted")
}

func downloadImageAttempt(ctx context.Context, rawURL string, client *http.Client) ([]byte, bool, error) {
	req, e := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if e != nil {
		return nil, false, failCtx(ctx, "generation.download.badURL")
	}
	resp, e := client.Do(req)
	if e != nil {
		var networkError net.Error
		retryable := errors.As(e, &networkError) || errors.Is(e, io.EOF) || errors.Is(e, io.ErrUnexpectedEOF)
		// Never include the signed result URL in errors shown in the interface.
		return nil, retryable, failCtx(ctx, "generation.download.connectFailed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		retryable := resp.StatusCode == 408 || resp.StatusCode == 429 || resp.StatusCode >= 500 && resp.StatusCode <= 599
		if resp.StatusCode == 401 || resp.StatusCode == 403 || resp.StatusCode == 404 || resp.StatusCode == 410 {
			return nil, false, failCtx(ctx, "generation.download.expired", "status", resp.StatusCode)
		}
		return nil, retryable, failCtx(ctx, "generation.download.httpStatus", "status", resp.StatusCode)
	}
	if resp.ContentLength > MaxImageBytes {
		return nil, false, failCtx(ctx, "generation.download.tooLarge")
	}
	b, e := io.ReadAll(io.LimitReader(resp.Body, MaxImageBytes+1))
	if len(b) > MaxImageBytes {
		return nil, false, failCtx(ctx, "generation.download.tooLarge")
	}
	if e != nil {
		return nil, true, failCtx(ctx, "generation.download.cut", "bytes", len(b))
	}
	return b, false, nil
}
