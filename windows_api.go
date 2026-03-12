package main

import (
	"syscall"
	"unsafe"
)

// Windows API
var (
	user32                  = syscall.NewLazyDLL("user32.dll")
	procGetForegroundWindow = user32.NewProc("GetForegroundWindow")
	procGetWindowRect       = user32.NewProc("GetWindowRect")
	procSetWindowPos        = user32.NewProc("SetWindowPos")
	procFindWindowW         = user32.NewProc("FindWindowW")
	procShowWindow          = user32.NewProc("ShowWindow")
)

type winRect struct {
	Left, Top, Right, Bottom int32
}

const swpNoZOrder = 0x0004

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

func findWindowByTitle(title string) uintptr {
	titlePtr, _ := syscall.UTF16PtrFromString(title)
	hwnd, _, _ := procFindWindowW.Call(0, uintptr(unsafe.Pointer(titlePtr)))
	return hwnd
}

const swHide = 0
const swShow = 5

func showWindow(hwnd uintptr, cmd int) bool {
	ret, _, _ := procShowWindow.Call(hwnd, uintptr(cmd))
	return ret != 0
}
