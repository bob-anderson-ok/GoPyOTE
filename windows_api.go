package main

import (
	"runtime"
	"syscall"
	"unsafe"
)

// Windows API
var (
	user32                    = syscall.NewLazyDLL("user32.dll")
	procGetForegroundWindow   = user32.NewProc("GetForegroundWindow")
	procGetWindowRect         = user32.NewProc("GetWindowRect")
	procSetWindowPos          = user32.NewProc("SetWindowPos")
	procSystemParametersInfoW = user32.NewProc("SystemParametersInfoW")
	procGetDpiForSystem       = user32.NewProc("GetDpiForSystem")
	procFindWindowW           = user32.NewProc("FindWindowW")
)

type winRect struct {
	Left, Top, Right, Bottom int32
}

const (
	swpNoZOrder    = 0x0004
	spiGetWorkArea = 0x0030
	// hwndTopmost is HWND_TOPMOST ((HWND)-1): place the window above all
	// non-topmost windows.
	hwndTopmost = ^uintptr(0)
)

func getForegroundWindow() uintptr {
	hwnd, _, _ := procGetForegroundWindow.Call()
	return hwnd
}

func getWindowRect(hwnd uintptr) (x, y, w, h int32, ok bool) {
	var rect winRect
	ret, _, _ := procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&rect)))
	if ret == 0 {
		return 0, 0, 0, 0, false
	}
	return rect.Left, rect.Top, rect.Right - rect.Left, rect.Bottom - rect.Top, true
}

func setWindowPos(hwnd uintptr, x, y, w, h int32) bool {
	ret, _, _ := procSetWindowPos.Call(hwnd, 0, uintptr(x), uintptr(y), uintptr(w), uintptr(h), swpNoZOrder)
	return ret != 0
}

// setWindowPosTopmost moves the window and raises it above all non-topmost
// windows, so a notice cannot end up behind the plot windows it is covering for.
func setWindowPosTopmost(hwnd uintptr, x, y, w, h int32) bool {
	ret, _, _ := procSetWindowPos.Call(hwnd, hwndTopmost, uintptr(x), uintptr(y), uintptr(w), uintptr(h), 0)
	return ret != 0
}

// findWindowByTitle returns the handle of the top-level window with exactly this
// title, or 0 if there is none. Used to find a window we just created without
// assuming it is the foreground window — which it may not be yet, and grabbing
// the wrong handle would move some other application's window.
func findWindowByTitle(title string) uintptr {
	ptr, err := syscall.UTF16PtrFromString(title)
	if err != nil {
		return 0
	}
	hwnd, _, _ := procFindWindowW.Call(0, uintptr(unsafe.Pointer(ptr)))
	runtime.KeepAlive(ptr)
	return hwnd
}

// getSystemDpi returns the system DPI (96 = 100%, 144 = 150%, 192 = 200%).
// Falls back to 96 on Windows versions older than 1607 where GetDpiForSystem is unavailable.
func getSystemDpi() uint32 {
	if err := procGetDpiForSystem.Find(); err != nil {
		return 96
	}
	ret, _, _ := procGetDpiForSystem.Call()
	if ret == 0 {
		return 96
	}
	return uint32(ret)
}

// getWorkArea returns the primary monitor's work area (screen minus taskbar) in pixels.
func getWorkArea() (x, y, w, h int32, ok bool) {
	var rect winRect
	ret, _, _ := procSystemParametersInfoW.Call(
		uintptr(spiGetWorkArea),
		0,
		uintptr(unsafe.Pointer(&rect)),
		0,
	)
	if ret == 0 {
		return 0, 0, 0, 0, false
	}
	return rect.Left, rect.Top, rect.Right - rect.Left, rect.Bottom - rect.Top, true
}
