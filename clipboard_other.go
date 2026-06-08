//go:build !windows

package main

import "fmt"

func writeWindowsHTMLClipboard(content string) error {
	return fmt.Errorf("Windows HTML 剪贴板仅支持 Windows")
}
