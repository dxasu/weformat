//go:build windows

package main

import (
	"fmt"
	"html"
	"regexp"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

const (
	gmemMoveable  = 0x0002
	cfUnicodeText = 13
)

var (
	user32                = syscall.NewLazyDLL("user32.dll")
	kernel32              = syscall.NewLazyDLL("kernel32.dll")
	procRegisterClipboard = user32.NewProc("RegisterClipboardFormatW")
	procOpenClipboard     = user32.NewProc("OpenClipboard")
	procEmptyClipboard    = user32.NewProc("EmptyClipboard")
	procSetClipboardData  = user32.NewProc("SetClipboardData")
	procCloseClipboard    = user32.NewProc("CloseClipboard")
	procGlobalAlloc       = kernel32.NewProc("GlobalAlloc")
	procGlobalLock        = kernel32.NewProc("GlobalLock")
	procGlobalUnlock      = kernel32.NewProc("GlobalUnlock")
	procGlobalFree        = kernel32.NewProc("GlobalFree")
	procRtlMoveMemory     = kernel32.NewProc("RtlMoveMemory")
)

func winErr(action string, err error) error {
	if errno, ok := err.(syscall.Errno); ok && errno == 0 {
		return fmt.Errorf("%s 失败", action)
	}
	return fmt.Errorf("%s 失败: %w", action, err)
}

func openClipboardWithRetry() error {
	var lastErr error
	for i := 0; i < 10; i++ {
		r, _, err := procOpenClipboard.Call(0)
		if r != 0 {
			return nil
		}
		lastErr = err
		time.Sleep(20 * time.Millisecond)
	}
	return winErr("打开剪贴板", lastErr)
}

func setClipboardBytes(format uintptr, data []byte) error {
	handle, _, err := procGlobalAlloc.Call(gmemMoveable, uintptr(len(data)))
	if handle == 0 {
		return winErr("分配剪贴板内存", err)
	}

	locked, _, err := procGlobalLock.Call(handle)
	if locked == 0 {
		procGlobalFree.Call(handle)
		return winErr("锁定剪贴板内存", err)
	}
	procRtlMoveMemory.Call(locked, uintptr(unsafe.Pointer(&data[0])), uintptr(len(data)))
	procGlobalUnlock.Call(handle)

	if r, _, err := procSetClipboardData.Call(format, handle); r == 0 {
		procGlobalFree.Call(handle)
		return winErr("写入剪贴板", err)
	}

	return nil
}

func unicodeTextBytes(text string) []byte {
	utf16Text := syscall.StringToUTF16(text)
	data := make([]byte, len(utf16Text)*2)
	for i, r := range utf16Text {
		data[i*2] = byte(r)
		data[i*2+1] = byte(r >> 8)
	}
	return data
}

func plainTextFromHTML(content string) string {
	text := regexp.MustCompile(`(?is)<script[^>]*>.*?</script>`).ReplaceAllString(content, "")
	text = regexp.MustCompile(`(?is)<style[^>]*>.*?</style>`).ReplaceAllString(text, "")
	text = regexp.MustCompile(`(?i)<br\s*/?>`).ReplaceAllString(text, "\n")
	text = regexp.MustCompile(`(?i)</(p|div|section|article|header|footer|h[1-6]|blockquote|pre|table|tr|ul|ol|li)>`).ReplaceAllString(text, "\n")
	text = regexp.MustCompile(`(?s)<[^>]+>`).ReplaceAllString(text, "")
	text = html.UnescapeString(text)
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	text = regexp.MustCompile(`[ \t]+\n`).ReplaceAllString(text, "\n")
	text = regexp.MustCompile(`\n[ \t]+`).ReplaceAllString(text, "\n")
	text = regexp.MustCompile(`[ \t]{2,}`).ReplaceAllString(text, " ")
	text = regexp.MustCompile(`\n{3,}`).ReplaceAllString(text, "\n\n")
	return strings.TrimSpace(text)
}

func writeWindowsHTMLClipboard(content string) error {
	formatName, err := syscall.UTF16PtrFromString("HTML Format")
	if err != nil {
		return err
	}
	format, _, err := procRegisterClipboard.Call(uintptr(unsafe.Pointer(formatName)))
	if format == 0 {
		return winErr("注册 HTML 剪贴板格式", err)
	}

	if err := openClipboardWithRetry(); err != nil {
		return err
	}
	defer procCloseClipboard.Call()

	if r, _, err := procEmptyClipboard.Call(); r == 0 {
		return winErr("清空剪贴板", err)
	}

	if err := setClipboardBytes(format, append([]byte(buildCFHTML(content)), 0)); err != nil {
		return err
	}
	if err := setClipboardBytes(cfUnicodeText, unicodeTextBytes(plainTextFromHTML(content))); err != nil {
		return err
	}

	return nil
}
