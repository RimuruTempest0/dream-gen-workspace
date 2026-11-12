//go:build windows

package main

import (
	"strings"
	"syscall"
	"unsafe"

	"github.com/wailsapp/go-webview2/pkg/edge"
	"golang.org/x/sys/windows"
)

// The helper only reads one public page. These guards keep the page from doing
// anything else in the bundled runtime: no new windows or popups, no downloads,
// no navigation to anything but http/https, no saved passwords or form data, and
// every permission request (camera, location, notifications…) denied. Each block
// is reported as a "blocked" event so tests can see it.
//
// go-webview2 v1.0.22 does not wrap these events, so the handlers are small COM
// objects built here (vtable: QueryInterface, AddRef, Release, Invoke).

type comHandler struct {
	vtbl   *comHandlerVtbl
	invoke func(args unsafe.Pointer)
}

type comHandlerVtbl struct{ QueryInterface, AddRef, Release, Invoke uintptr }

var handlerVtbl = &comHandlerVtbl{
	QueryInterface: syscall.NewCallback(func(this, _ uintptr, out *uintptr) uintptr { *out = this; return 0 }),
	AddRef:         syscall.NewCallback(func(uintptr) uintptr { return 1 }),
	Release:        syscall.NewCallback(func(uintptr) uintptr { return 1 }),
	Invoke: syscall.NewCallback(func(this *comHandler, _, args unsafe.Pointer) uintptr {
		this.invoke(args)
		return 0
	}),
}

// handlers stay referenced for the life of the process: WebView2 keeps pointers.
var handlers []*comHandler

func newHandler(invoke func(args unsafe.Pointer)) uintptr {
	h := &comHandler{vtbl: handlerVtbl, invoke: invoke}
	handlers = append(handlers, h)
	return uintptr(unsafe.Pointer(h))
}

// ICoreWebView2 through ICoreWebView2_4, in vtable order (only the slots used here are named).
type webviewVtbl struct {
	QueryInterface, AddRef, Release                                                  uintptr
	GetSettings, GetSource, Navigate, NavigateToString                               uintptr
	AddNavigationStarting, RemoveNavigationStarting                                  uintptr
	_                                                                                [35]uintptr // ContentLoading … Stop
	AddNewWindowRequested, RemoveNewWindowRequested                                  uintptr
	_                                                                                [15]uintptr // DocumentTitleChanged … WindowCloseRequested
	_                                                                                [7]uintptr  // ICoreWebView2_2
	_                                                                                [5]uintptr  // ICoreWebView2_3
	AddFrameCreated, RemoveFrameCreated, AddDownloadStarting, RemoveDownloadStarting uintptr
}

var (
	iidWebView2_4 = windows.GUID{Data1: 0x20D02D59, Data2: 0x6DF2, Data3: 0x42DC, Data4: [8]byte{0xBD, 0x06, 0xF9, 0x8A, 0x69, 0x4B, 0x13, 0x02}}
	ole32         = windows.NewLazySystemDLL("ole32.dll")
	pTaskMemFree  = ole32.NewProc("CoTaskMemFree")
)

// argsVtbl reads method n (0-based, after IUnknown's three) of an event-args object.
func argsMethod(args unsafe.Pointer, n int) uintptr {
	vtbl := *(*unsafe.Pointer)(args)
	return *(*uintptr)(unsafe.Add(vtbl, uintptr(3+n)*unsafe.Sizeof(uintptr(0))))
}

func getURI(args unsafe.Pointer) string {
	var p *uint16
	if hr, _, _ := syscall.SyscallN(argsMethod(args, 0), uintptr(args), uintptr(unsafe.Pointer(&p))); hr != 0 || p == nil {
		return ""
	}
	defer pTaskMemFree.Call(uintptr(unsafe.Pointer(p)))
	return windows.UTF16PtrToString(p)
}

// allowedNavigation is http and https only (about:blank is the empty start page).
func allowedNavigation(uri string) bool {
	u := strings.ToLower(uri)
	return strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://") || u == "about:blank"
}

func installGuards(chromium *edge.Chromium) {
	_ = chromium.PutIsPasswordAutosaveEnabled(false)
	_ = chromium.PutIsGeneralAutofillEnabled(false)
	chromium.SetGlobalPermission(edge.CoreWebView2PermissionStateDeny)
	controller := chromium.GetController()
	if controller == nil {
		return
	}
	webview, e := controller.GetCoreWebView2()
	if e != nil || webview == nil {
		return
	}
	this := uintptr(unsafe.Pointer(webview))
	vtbl := *(**webviewVtbl)(unsafe.Pointer(webview))
	var token int64

	// NewWindowRequested args: Uri, put_NewWindow, get_NewWindow, put_Handled …
	syscall.SyscallN(vtbl.AddNewWindowRequested, this, newHandler(func(args unsafe.Pointer) {
		uri := getURI(args)
		syscall.SyscallN(argsMethod(args, 3), uintptr(args), 1) // put_Handled(TRUE) without a window: nothing opens
		emit(map[string]any{"event": "blocked", "what": "window", "url": uri})
	}), uintptr(unsafe.Pointer(&token)))

	// NavigationStarting args: Uri, IsUserInitiated, IsRedirected, RequestHeaders, get_Cancel, put_Cancel …
	syscall.SyscallN(vtbl.AddNavigationStarting, this, newHandler(func(args unsafe.Pointer) {
		if uri := getURI(args); !allowedNavigation(uri) {
			syscall.SyscallN(argsMethod(args, 5), uintptr(args), 1) // put_Cancel(TRUE)
			emit(map[string]any{"event": "blocked", "what": "navigation", "url": uri})
		}
	}), uintptr(unsafe.Pointer(&token)))

	// DownloadStarting (ICoreWebView2_4) args: DownloadOperation, get_Cancel, put_Cancel …
	var v4 unsafe.Pointer
	if hr, _, _ := syscall.SyscallN(vtbl.QueryInterface, this, uintptr(unsafe.Pointer(&iidWebView2_4)), uintptr(unsafe.Pointer(&v4))); hr == 0 && v4 != nil {
		v4tbl := *(**webviewVtbl)(v4)
		syscall.SyscallN(v4tbl.AddDownloadStarting, uintptr(v4), newHandler(func(args unsafe.Pointer) {
			syscall.SyscallN(argsMethod(args, 2), uintptr(args), 1) // put_Cancel(TRUE)
			emit(map[string]any{"event": "blocked", "what": "download"})
		}), uintptr(unsafe.Pointer(&token)))
	}
}
