//go:build windows

// product-browser loads one public product page in the bundled WebView2 and
// returns the rendered HTML to ImageWorkbench. It only reads pages: the caller
// validates every URL, the window is shown only when a human check stays on
// screen, and nothing from the page is executed by the caller.
//
// Protocol: one JSON object per stdout line (see emit calls). After the page
// event the helper reads {"op":"image","id","url"} / {"op":"quit"} lines on
// stdin and answers image requests from the page session. Stdin EOF exits.
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"runtime"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/wailsapp/go-webview2/pkg/edge"
	"golang.org/x/sys/windows"
)

var (
	user32            = windows.NewLazySystemDLL("user32.dll")
	kernel32          = windows.NewLazySystemDLL("kernel32.dll")
	pRegisterClassEx  = user32.NewProc("RegisterClassExW")
	pCreateWindowEx   = user32.NewProc("CreateWindowExW")
	pDefWindowProc    = user32.NewProc("DefWindowProcW")
	pShowWindow       = user32.NewProc("ShowWindow")
	pSetForeground    = user32.NewProc("SetForegroundWindow")
	pGetMessage       = user32.NewProc("GetMessageW")
	pTranslateMessage = user32.NewProc("TranslateMessage")
	pDispatchMessage  = user32.NewProc("DispatchMessageW")
	pPostMessage      = user32.NewProc("PostMessageW")
	pGetModuleHandle  = kernel32.NewProc("GetModuleHandleW")
)

const (
	wmSize   = 0x0005
	wmClose  = 0x0010
	wmRun    = 0x8001 // WM_APP+1: run queued work on the UI thread
	swShow   = 5
	wsWindow = 0x00CF0000 // WS_OVERLAPPEDWINDOW
)

type wndClassEx struct {
	Size, Style                        uint32
	WndProc                            uintptr
	ClsExtra, WndExtra                 int32
	Instance, Icon, Cursor, Background uintptr
	MenuName, ClassName                *uint16
	IconSm                             uintptr
}
type winMsg struct {
	Hwnd           uintptr
	Message        uint32
	WParam, LParam uintptr
	Time           uint32
	X, Y           int32
}

var (
	outMu sync.Mutex
	out   = bufio.NewWriterSize(os.Stdout, 1<<20)
)

func emit(v map[string]any) {
	b, _ := json.Marshal(v)
	outMu.Lock()
	out.Write(b)
	out.WriteByte('\n')
	out.Flush()
	outMu.Unlock()
}

// A link the browser cannot reach is loaded again after 3 s, then 6 s.
const (
	maxNavAttempts = 3
	navRetryDelay  = 3 * time.Second
)

// iidNavigationCompletedArgs2 is ICoreWebView2NavigationCompletedEventArgs2, which
// adds the HTTP status code of the navigation.
var iidNavigationCompletedArgs2 = windows.GUID{Data1: 0xFDF8B738, Data2: 0xEE1E, Data3: 0x4DB2, Data4: [8]byte{0xA3, 0x29, 0x8D, 0x7D, 0x7B, 0x74, 0xD7, 0x92}}

// unreachable tells a site that could not be reached at all (no connection, name
// not resolved, connection dropped) from a page the site answered, such as a 403
// verification page: only the first ends without any HTTP status. WebView2 reports
// such failures with WebErrorStatus UNKNOWN, so the status code decides.
// go-webview2 declares these interfaces but does not export their getters.
func unreachable(args *edge.ICoreWebView2NavigationCompletedEventArgs) bool {
	this := uintptr(unsafe.Pointer(args))
	vtbl := *(**struct {
		QueryInterface, AddRef, Release, GetIsSuccess, GetWebErrorStatus, GetNavigationID uintptr
	})(unsafe.Pointer(args))
	var ok, status int32
	syscall.SyscallN(vtbl.GetIsSuccess, this, uintptr(unsafe.Pointer(&ok)))
	syscall.SyscallN(vtbl.GetWebErrorStatus, this, uintptr(unsafe.Pointer(&status)))
	if ok != 0 || status == 14 { // succeeded, or replaced by another navigation
		return false
	}
	var v2 unsafe.Pointer
	if hr, _, _ := syscall.SyscallN(vtbl.QueryInterface, this, uintptr(unsafe.Pointer(&iidNavigationCompletedArgs2)), uintptr(unsafe.Pointer(&v2))); hr != 0 || v2 == nil {
		return false
	}
	vtbl2 := *(**struct {
		QueryInterface, AddRef, Release, GetIsSuccess, GetWebErrorStatus, GetNavigationID, GetHttpStatusCode uintptr
	})(v2)
	var code int32
	hr, _, _ := syscall.SyscallN(vtbl2.GetHttpStatusCode, uintptr(v2), uintptr(unsafe.Pointer(&code)))
	syscall.SyscallN(vtbl2.Release, uintptr(v2))
	return hr == 0 && code == 0
}

func fail(code, message string) {
	emit(map[string]any{"event": "error", "code": code, "message": message})
	os.Exit(1)
}

func main() {
	target := flag.String("url", "", "")
	browser := flag.String("runtime", "", "")
	profile := flag.String("profile", "", "")
	timeout := flag.Int("timeout", 180, "")
	challengeAfter := flag.Int("challenge-after", 20, "")
	maxHTML := flag.Int("max-html", 4<<20, "")
	maxImage := flag.Int("max-image", 32<<20, "")
	flag.Parse()
	if *target == "" || *profile == "" {
		fail("arguments", "缺少页面或资料目录参数")
	}
	runtime.LockOSThread()
	start := time.Now()

	var (
		mu        sync.Mutex
		pageDone  bool
		shown     bool
		challenge time.Time
		queue     []func()
		chromium  *edge.Chromium
		hwnd      uintptr
		navFails  int
	)
	go func() {
		time.Sleep(time.Duration(*timeout) * time.Second)
		mu.Lock()
		done := pageDone
		mu.Unlock()
		if !done {
			fail("timeout", "读取商品页超时")
		}
	}()

	instance, _, _ := pGetModuleHandle.Call(0)
	className, _ := syscall.UTF16PtrFromString("ImageWorkbenchProductBrowser")
	wndProc := syscall.NewCallback(func(h, m, w, l uintptr) uintptr {
		switch m {
		case wmSize:
			if chromium != nil {
				chromium.Resize()
			}
		case wmClose:
			fail("closed", "验证窗口已关闭")
		case wmRun:
			mu.Lock()
			jobs := queue
			queue = nil
			mu.Unlock()
			for _, job := range jobs {
				job()
			}
			return 0
		}
		r, _, _ := pDefWindowProc.Call(h, m, w, l)
		return r
	})
	wc := wndClassEx{WndProc: wndProc, Instance: instance, ClassName: className}
	wc.Size = uint32(unsafe.Sizeof(wc))
	pRegisterClassEx.Call(uintptr(unsafe.Pointer(&wc)))
	title, _ := syscall.UTF16PtrFromString("完成验证后将自动继续 - 生图工作台")
	hwnd, _, _ = pCreateWindowEx.Call(0, uintptr(unsafe.Pointer(className)), uintptr(unsafe.Pointer(title)), wsWindow, 120, 80, 1100, 820, 0, 0, instance, 0)
	if hwnd == 0 {
		fail("window", "无法创建浏览器窗口")
	}
	onUI := func(job func()) {
		mu.Lock()
		queue = append(queue, job)
		mu.Unlock()
		pPostMessage.Call(hwnd, wmRun, 0, 0)
	}

	chromium = edge.NewChromium()
	chromium.BrowserPath = *browser
	chromium.DataPath = *profile
	chromium.SetErrorCallback(func(e error) { fail("webview", e.Error()) })
	chromium.ProcessFailedCallback = func(*edge.ICoreWebView2, *edge.ICoreWebView2ProcessFailedEventArgs) {
		fail("crashed", "浏览器进程异常退出")
	}
	chromium.MessageCallback = func(message string, _ *edge.ICoreWebView2, _ *edge.ICoreWebView2WebMessageReceivedEventArgs) {
		var m struct {
			Type      string `json:"type"`
			Challenge bool   `json:"challenge"`
			URL       string `json:"url"`
			HTML      string `json:"html"`
			ID        string `json:"id"`
			Status    int    `json:"status"`
			MIME      string `json:"mime"`
			Data      string `json:"data"`
			Error     string `json:"error"`
		}
		if json.Unmarshal([]byte(message), &m) != nil {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		switch m.Type {
		case "state":
			if pageDone {
				return
			}
			if !m.Challenge {
				challenge = time.Time{}
				return
			}
			if challenge.IsZero() {
				challenge = time.Now()
				emit(map[string]any{"event": "challenge"})
			}
			if !shown && time.Since(challenge) >= time.Duration(*challengeAfter)*time.Second {
				shown = true
				pShowWindow.Call(hwnd, swShow)
				pSetForeground.Call(hwnd)
				emit(map[string]any{"event": "verify"})
			}
		case "page":
			if pageDone {
				return
			}
			if len(m.HTML) > *maxHTML {
				fail("too_large", "商品页超过大小限制")
			}
			pageDone = true
			pShowWindow.Call(hwnd, 0)
			emit(map[string]any{"event": "page", "url": m.URL, "html": m.HTML, "elapsedMs": time.Since(start).Milliseconds()})
		case "image":
			if len(m.Data)/4*3 > *maxImage {
				emit(map[string]any{"event": "image", "id": m.ID, "error": "图片超过大小限制"})
				return
			}
			emit(map[string]any{"event": "image", "id": m.ID, "status": m.Status, "mime": m.MIME, "data": m.Data, "error": m.Error})
		}
	}
	// When the site cannot be reached (a dropped connection, a proxy hiccup) WebView2
	// shows its own error page. Load the link again a few times before giving up;
	// the error page is never reported as the product page.
	chromium.NavigationCompletedCallback = func(_ *edge.ICoreWebView2, args *edge.ICoreWebView2NavigationCompletedEventArgs) {
		if !unreachable(args) {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		if pageDone {
			return
		}
		navFails++
		if navFails >= maxNavAttempts {
			fail("network", fmt.Sprintf("浏览器也打不开商品页（网络连接失败，已重试 %d 次）", navFails))
		}
		emit(map[string]any{"event": "retry", "attempt": navFails})
		wait := time.Duration(navFails) * navRetryDelay
		go func() {
			time.Sleep(wait)
			onUI(func() { chromium.Navigate(*target) })
		}()
	}
	if !chromium.Embed(hwnd) {
		fail("webview", "无法启动浏览器组件")
	}
	if settings, e := chromium.GetSettings(); e == nil {
		settings.PutAreDevToolsEnabled(false)
		settings.PutAreDefaultContextMenusEnabled(false)
		settings.PutAreDefaultScriptDialogsEnabled(false)
		settings.PutAreHostObjectsAllowed(false)
		settings.PutIsStatusBarEnabled(false)
	}
	chromium.Resize()
	chromium.Init(pageScript)
	chromium.Navigate(*target)
	emit(map[string]any{"event": "started"})

	go func() {
		scanner := bufio.NewScanner(os.Stdin)
		scanner.Buffer(make([]byte, 4096), 1<<20)
		for scanner.Scan() {
			var op struct{ Op, ID, URL string }
			if json.Unmarshal(bytes.TrimPrefix(scanner.Bytes(), []byte("\xef\xbb\xbf")), &op) != nil {
				continue
			}
			if op.Op == "quit" {
				break
			}
			if op.Op == "image" {
				arg, _ := json.Marshal([]string{op.ID, op.URL})
				onUI(func() {
					chromium.Eval("window.__imageWorkbenchImage && window.__imageWorkbenchImage(..." + string(arg) + ")")
				})
			}
		}
		os.Exit(0)
	}()

	var m winMsg
	for {
		r, _, _ := pGetMessage.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if r == 0 || int32(r) == -1 {
			break
		}
		pTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		pDispatchMessage.Call(uintptr(unsafe.Pointer(&m)))
	}
	os.Exit(0)
}

// pageScript reports whether the top document is a human-check page and, once
// the rendered content is stable, posts the HTML. A page that exposes Product
// JSON-LD is ready after two equal polls; other pages after three stable polls.
const pageScript = `(function(){
if (window.top !== window) return;
const post = o => window.chrome.webview.postMessage(JSON.stringify(o));
const challengeText = /(enable JS and disable any ad blocker|verify you are (a )?human|checking your browser|just a moment\.\.\.|press (&|and) hold|click the button below to continue shopping|enter the characters you see below)/i;
const challengeFrame = 'iframe[src*="captcha-delivery"],iframe[src*="challenges.cloudflare.com"],iframe[src*="hcaptcha.com"],iframe[src*="recaptcha"],#px-captcha,form[action*="validateCaptcha"]';
function hasProduct(){
  for (const s of document.querySelectorAll('script[type="application/ld+json"]')) {
    try {
      const all = [];
      (function walk(x){ if (Array.isArray(x)) x.forEach(walk); else if (x && typeof x === 'object') { all.push(x); if (x['@graph']) walk(x['@graph']); } })(JSON.parse(s.textContent));
      if (all.some(x => [].concat(x['@type'] || []).some(t => String(t).includes('Product')))) return true;
    } catch (e) {}
  }
  return false;
}
let last = '', stable = 0, sent = false;
setInterval(function(){
  if (sent || !document.documentElement) return;
  // The browser's own error page is never the product; the helper loads the link again.
  if (!/^https?:$/.test(location.protocol)) return;
  const body = document.body ? document.body.innerText : '';
  const challenge = challengeText.test(body.slice(0, 4000)) || !!document.querySelector(challengeFrame);
  post({type: 'state', challenge});
  if (challenge) { stable = 0; last = ''; return; }
  const product = hasProduct();
  const key = product + ':' + body.length;
  stable = key === last ? stable + 1 : 0;
  last = key;
  if ((product && stable >= 1) || (document.readyState === 'complete' && body.trim().length > 0 && stable >= 2)) {
    sent = true;
    post({type: 'page', url: location.href, html: document.documentElement.outerHTML});
  }
}, 1500);
window.__imageWorkbenchImage = async function(id, url){
  try {
    const r = await fetch(url, {credentials: 'omit'});
    const bytes = new Uint8Array(await r.arrayBuffer());
    let bin = '';
    for (let i = 0; i < bytes.length; i += 32768) bin += String.fromCharCode.apply(null, bytes.subarray(i, i + 32768));
    post({type: 'image', id, status: r.status, mime: r.headers.get('content-type') || '', data: btoa(bin)});
  } catch (e) { post({type: 'image', id, error: String(e)}); }
};
})();`
