package core

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// The product browser is a separate helper executable that renders a public
// product page in the bundled WebView2 when the direct request is blocked.
// It only returns HTML and requested image bytes; the Agent never drives it.
const (
	productBrowserDir = "product-browser"
	// Stage texts are matched by the smart-generation page.
	productBrowserReadingStage = "正在用浏览器读取商品页"
	productBrowserVerifyStage  = "需要你在弹出的窗口里完成验证"
)

// Variables so tests can shorten waits and limits.
var (
	productBrowserChallengeAfter = 20 * time.Second
	// qaLocalProduct is set only by the desktop QA build (product_browser_qa.go).
	qaLocalProduct             = false
	productBrowserTimeout      = 3 * time.Minute
	productBrowserImageTimeout = time.Minute
	productBrowserMaxHTML      = 4 << 20
)

type productBrowserConfig struct {
	Executable string
	Runtime    string
	// Args is prepended to the helper flags; tests use it to re-run the test binary as a fake helper.
	Args []string
}

func (s *Service) SetProductBrowser(executable, runtimeDir string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.productBrowser = productBrowserConfig{Executable: executable, Runtime: runtimeDir}
}

// InheritProductBrowser copies the helper location when a service is replaced.
func (s *Service) InheritProductBrowser(from *Service) {
	from.mu.Lock()
	cfg := from.productBrowser
	from.mu.Unlock()
	s.mu.Lock()
	s.productBrowser = cfg
	s.mu.Unlock()
}

// productBrowserProfile keeps the helper's cookies in the current data directory.
// Backup and data migration copy named folders only, so it never leaves this machine.
func (s *Service) productBrowserProfile() string {
	return filepath.Join(s.store.Root, productBrowserDir)
}

type browserEvent struct {
	Event     string `json:"event"`
	Code      string `json:"code"`
	Message   string `json:"message"`
	URL       string `json:"url"`
	HTML      string `json:"html"`
	ID        string `json:"id"`
	Status    int    `json:"status"`
	MIME      string `json:"mime"`
	Data      string `json:"data"`
	Error     string `json:"error"`
	ElapsedMs int64  `json:"elapsedMs"`
	Attempt   int    `json:"attempt"`
}

type browserSession struct {
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	events  chan browserEvent
	done    chan struct{}
	stderr  cappedBuffer
	readErr error
	release func()
	onClose func()
	once    sync.Once
	nextID  int
}

func startProductBrowser(cfg productBrowserConfig, profile, target string) (*browserSession, error) {
	args := append(append([]string{}, cfg.Args...),
		"-url", target, "-runtime", cfg.Runtime, "-profile", profile,
		"-timeout", strconv.Itoa(int(productBrowserTimeout/time.Second)),
		"-challenge-after", strconv.Itoa(int(productBrowserChallengeAfter/time.Second)),
		"-max-html", strconv.Itoa(productBrowserMaxHTML), "-max-image", strconv.Itoa(MaxImageBytes))
	b := &browserSession{cmd: exec.Command(cfg.Executable, args...), events: make(chan browserEvent, 16), done: make(chan struct{})}
	b.cmd.Stderr = &b.stderr
	out, e := b.cmd.StdoutPipe()
	if e != nil {
		return nil, e
	}
	if b.stdin, e = b.cmd.StdinPipe(); e != nil {
		return nil, e
	}
	if e = b.cmd.Start(); e != nil {
		return nil, fmt.Errorf("无法启动浏览器辅助程序：%w", e)
	}
	if b.release, e = attachAgentProcess(b.cmd); e != nil {
		b.cmd.Process.Kill()
		b.cmd.Wait()
		return nil, e
	}
	allowBrowserForeground(b.cmd.Process.Pid)
	go func() {
		scanner := bufio.NewScanner(out)
		// One line carries at most a base64 image or the capped page HTML as JSON.
		scanner.Buffer(make([]byte, 65536), MaxImageBytes/3*4+(1<<20))
		for scanner.Scan() {
			var ev browserEvent
			if json.Unmarshal(scanner.Bytes(), &ev) == nil && ev.Event != "" {
				b.events <- ev
			}
		}
		if e := scanner.Err(); e != nil {
			b.readErr = errors.New("浏览器辅助程序输出超过大小限制")
			b.cmd.Process.Kill()
		}
		b.cmd.Wait()
		close(b.events)
		close(b.done)
	}()
	return b, nil
}

// Close asks the helper to quit and kills it if it does not exit promptly.
func (b *browserSession) Close() {
	b.once.Do(func() {
		io.WriteString(b.stdin, `{"op":"quit"}`+"\n")
		b.stdin.Close()
		select {
		case <-b.done:
		case <-time.After(5 * time.Second):
			b.cmd.Process.Kill()
			<-b.done
		}
		b.release()
		if b.onClose != nil {
			b.onClose()
		}
	})
}

func (b *browserSession) kill() {
	b.cmd.Process.Kill()
	b.Close()
}

func (b *browserSession) diagnostic() string {
	text := strings.TrimSpace(b.stderr.String())
	if len(text) > 600 {
		text = text[len(text)-600:]
	}
	return text
}

func browserFailure(ev browserEvent) error {
	switch ev.Code {
	case "timeout":
		return errors.New("浏览器读取超时")
	case "closed":
		return errors.New("验证窗口已关闭")
	case "crashed":
		return errors.New("浏览器进程异常退出")
	case "too_large":
		return errors.New("商品页超过大小限制")
	}
	if ev.Message != "" {
		return errors.New(cutProductText(ev.Message, 300))
	}
	return errors.New("浏览器读取失败")
}

// readProductWithBrowser returns the rendered page and an open session that can
// fetch images with the same cookies. The caller must Close the session.
func (s *Service) readProductWithBrowser(ctx context.Context, id, target string) ([]byte, string, *browserSession, error) {
	s.mu.Lock()
	cfg := s.productBrowser
	s.mu.Unlock()
	if cfg.Executable == "" {
		return nil, "", nil, errors.New("未配置浏览器辅助程序")
	}
	if info, e := os.Stat(cfg.Executable); e != nil || info.IsDir() {
		return nil, "", nil, errors.New("缺少浏览器辅助程序，请完整解压应用文件")
	}
	// One helper at a time: WebView2 locks its profile directory.
	s.browserMu.Lock()
	profile := s.productBrowserProfile()
	if e := os.MkdirAll(profile, 0700); e != nil {
		s.browserMu.Unlock()
		return nil, "", nil, e
	}
	b, e := startProductBrowser(cfg, profile, target)
	if e != nil {
		s.browserMu.Unlock()
		return nil, "", nil, e
	}
	// The returned session keeps the profile locked until it is closed.
	b.onClose = s.browserMu.Unlock
	s.updateAgent(id, productBrowserReadingStage, nil)
	deadline := time.NewTimer(productBrowserTimeout)
	defer deadline.Stop()
	for {
		select {
		case <-ctx.Done():
			b.kill()
			return nil, "", nil, ctx.Err()
		case <-deadline.C:
			b.kill()
			return nil, "", nil, errors.New("浏览器读取超时")
		case ev, ok := <-b.events:
			if !ok {
				failure := b.readErr
				if failure == nil {
					failure = errors.New("浏览器辅助程序意外退出")
				}
				diag := b.diagnostic()
				b.Close()
				if diag != "" {
					failure = fmt.Errorf("%w（%s）", failure, diag)
				}
				return nil, "", nil, failure
			}
			switch ev.Event {
			case "retry":
				_ = s.store.appendLog("warn", "agent", id, fmt.Sprintf("浏览器打不开商品页（网络连接失败），稍后第 %d 次重新打开", ev.Attempt))
			case "verify":
				_ = s.store.appendLog("warn", "agent", id, "商品页出现人机验证，已弹出验证窗口")
				// The page shows live waiting seconds from this event's time.
				s.updateAgent(id, productBrowserVerifyStage, nil)
			case "page":
				if len(ev.HTML) > productBrowserMaxHTML {
					b.kill()
					return nil, "", nil, errors.New("商品页超过大小限制")
				}
				return []byte(ev.HTML), ev.URL, b, nil
			case "error":
				diag := b.diagnostic()
				b.Close()
				failure := browserFailure(ev)
				if diag != "" && ev.Code == "webview" {
					failure = fmt.Errorf("%w（%s）", failure, diag)
				}
				return nil, "", nil, failure
			}
		}
	}
}

// productFromBrowser renders the page in the helper and parses it with the same
// parser as the direct request. Stages and errors are logged; page content is not.
func (s *Service) productFromBrowser(ctx context.Context, id, target, reason string) (ProductInfo, []string, *browserSession, error) {
	_ = s.store.appendLog("info", "agent", id, "商品页直连受阻（"+reason+"），改用浏览器读取："+target)
	started := time.Now()
	s.setProgress(id, func(p *RunProgress) { p.ProductVia = "browser" })
	page, finalURL, b, e := s.readProductWithBrowser(ctx, id, target)
	s.setProgress(id, func(p *RunProgress) { p.ProductBrowserMS = time.Since(started).Milliseconds() })
	if e == nil {
		if u, err := url.Parse(finalURL); err != nil || (u.Scheme != "http" && u.Scheme != "https") {
			// The browser's own error page: the site could not be reached.
			e = errors.New("浏览器也打不开商品页（网络连接失败）")
		} else if s.allowLocalProduct {
			e = validateProductURL(finalURL)
		} else {
			e = publicProductURL(ctx, finalURL)
		}
		if errors.Is(e, errProductTarget) {
			e = fmt.Errorf("页面跳转到不受支持的地址：%w", e)
		}
		if e != nil {
			b.Close()
		}
	}
	var info ProductInfo
	var images []string
	if e == nil {
		if info, images, e = parseProductPage(page, finalURL); e == nil && (looksLikeChallenge(page) || challengeInfo(info)) {
			// Never pass a block page on as product data.
			e = errors.New("浏览器中仍是人机验证页")
		}
		if e != nil {
			b.Close()
		}
	}
	if e != nil {
		if ctx.Err() == nil {
			_ = s.store.appendLog("warn", "agent", id, fmt.Sprintf("浏览器读取商品页失败（%d 秒）：%v", int(time.Since(started).Seconds()), e))
		}
		return ProductInfo{}, nil, nil, e
	}
	_ = s.store.appendLog("info", "agent", id, fmt.Sprintf("浏览器读取商品页完成（%d 秒）；页面 %d KB，候选图片 %d 张", int(time.Since(started).Seconds()), len(page)>>10, len(images)))
	return info, images, b, nil
}

// Image fetches one image inside the page session, used only after a direct download failed.
func (b *browserSession) Image(ctx context.Context, target string) ([]byte, error) {
	b.nextID++
	id := strconv.Itoa(b.nextID)
	line, _ := json.Marshal(map[string]string{"op": "image", "id": id, "url": target})
	if _, e := b.stdin.Write(append(line, '\n')); e != nil {
		return nil, errors.New("浏览器会话已结束")
	}
	timer := time.NewTimer(productBrowserImageTimeout)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timer.C:
			return nil, errors.New("浏览器补取图片超时")
		case ev, ok := <-b.events:
			if !ok {
				return nil, errors.New("浏览器会话已结束")
			}
			if ev.Event != "image" || ev.ID != id {
				continue
			}
			if ev.Error != "" {
				return nil, errors.New(cutProductText(ev.Error, 300))
			}
			if ev.Status != 200 {
				return nil, fmt.Errorf("浏览器补取图片 HTTP %d", ev.Status)
			}
			data, e := base64.StdEncoding.DecodeString(ev.Data)
			if e != nil {
				return nil, e
			}
			if len(data) > MaxImageBytes {
				return nil, errors.New("图片超过大小限制")
			}
			return data, nil
		}
	}
}

// ClearProductBrowserData removes saved cookies and cache of the product browser.
func (s *Service) ClearProductBrowserData() error {
	if !s.browserMu.TryLock() {
		return errors.New("正在读取商品页，请稍后再清除")
	}
	defer s.browserMu.Unlock()
	return os.RemoveAll(s.productBrowserProfile())
}

type productHTTPError struct{ Status int }

func (e productHTTPError) Error() string {
	return fmt.Sprintf("商品资料 HTTP %d；请上传图片或粘贴商品描述", e.Status)
}

var challengeMarkers = [][]byte{
	[]byte("captcha-delivery.com"), []byte("enable JS and disable any ad blocker"),
	[]byte("challenges.cloudflare.com"), []byte("cf-chl-"), []byte("px-captcha"),
	// Walmart's PerimeterX block page.
	[]byte("<title>Robot or human?</title>"),
	// Amazon's "Continue shopping" and character captcha pages.
	[]byte("/errors/validateCaptcha"),
}

// challengeTexts are matched against the page text a parser extracted, so a block
// page is caught even when its markup changes.
var challengeTexts = []string{
	"click the button below to continue shopping",
	"enter the characters you see below",
	"type the characters you see in this image",
}

func looksLikeChallenge(page []byte) bool {
	for _, m := range challengeMarkers {
		if bytes.Contains(page, m) {
			return true
		}
	}
	return false
}

// challengeInfo reports whether parsed product data is really a block page.
func challengeInfo(info ProductInfo) bool {
	text := strings.ToLower(strings.Join(strings.Fields(info.Title+" "+info.Description), " "))
	for _, t := range challengeTexts {
		if strings.Contains(text, t) {
			return true
		}
	}
	return false
}

// browserFallbackReason explains why the direct result should be retried in the
// browser, or returns "" when a browser would not help (invalid or private target,
// missing page, cancellation).
func browserFallbackReason(ctx context.Context, page []byte, info ProductInfo, e error) string {
	if ctx.Err() != nil {
		return ""
	}
	var status productHTTPError
	switch {
	case errors.As(e, &status):
		switch status.Status {
		case 401, 403, 429, 503:
			return fmt.Sprintf("HTTP %d", status.Status)
		}
		return ""
	case errors.Is(e, errProductTarget), errors.Is(e, errProductTooLarge):
		return ""
	case e != nil:
		return "请求失败"
	case looksLikeChallenge(page), challengeInfo(info):
		return "返回人机验证页"
	case info.Error != "":
		return "页面缺少商品资料"
	}
	return ""
}
