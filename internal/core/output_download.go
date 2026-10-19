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
			return errors.New("图片重定向次数过多")
		}
		if r.URL.Scheme != "http" && r.URL.Scheme != "https" || r.URL.User != nil {
			return errors.New("不支持图片地址协议或认证信息")
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
		return nil, errors.New("结果图片地址无效")
	}
	for attempt := 1; attempt <= imageDownloadAttempts; attempt++ {
		data, retryable, err := downloadImageAttempt(ctx, rawURL, client)
		if err == nil {
			return data, nil
		}
		if ctx.Err() != nil {
			return nil, imageDownloadContextError(ctx.Err())
		}
		if !retryable || attempt == imageDownloadAttempts {
			return nil, fmt.Errorf("%w（已尝试 %d 次下载）。图片链接已保留，可点“补下载”，不会重新生图", err, attempt)
		}
		timer := time.NewTimer(retryDelay * time.Duration(attempt))
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, imageDownloadContextError(ctx.Err())
		case <-timer.C:
		}
	}
	panic("unreachable")
}

func imageDownloadContextError(err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return errors.New("图片下载等待已达 10 分钟，尚未保存完成。图片链接已保留，可点“补下载”，不会重新生图")
	}
	return errors.New("图片下载已中止。图片链接已保留，可点“补下载”，不会重新生图")
}

func downloadImageAttempt(ctx context.Context, rawURL string, client *http.Client) ([]byte, bool, error) {
	req, e := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if e != nil {
		return nil, false, errors.New("结果图片地址无效")
	}
	resp, e := client.Do(req)
	if e != nil {
		var networkError net.Error
		retryable := errors.As(e, &networkError) || errors.Is(e, io.EOF) || errors.Is(e, io.ErrUnexpectedEOF)
		// Never include the signed result URL in errors shown in the interface.
		return nil, retryable, errors.New("图片下载连接失败，请检查网络或代理")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		retryable := resp.StatusCode == 408 || resp.StatusCode == 429 || resp.StatusCode >= 500 && resp.StatusCode <= 599
		if resp.StatusCode == 401 || resp.StatusCode == 403 || resp.StatusCode == 404 || resp.StatusCode == 410 {
			return nil, false, fmt.Errorf("图片链接不可访问或已过期（HTTP %d），请核对中转站记录", resp.StatusCode)
		}
		return nil, retryable, fmt.Errorf("图片下载服务返回 HTTP %d", resp.StatusCode)
	}
	if resp.ContentLength > MaxImageBytes {
		return nil, false, errors.New("结果图片超过 32 MB")
	}
	b, e := io.ReadAll(io.LimitReader(resp.Body, MaxImageBytes+1))
	if len(b) > MaxImageBytes {
		return nil, false, errors.New("结果图片超过 32 MB")
	}
	if e != nil {
		return nil, true, fmt.Errorf("图片文件下载中断（已接收 %d 字节）", len(b))
	}
	return b, false, nil
}
