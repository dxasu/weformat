//go:build !windows

package clipboard

import "fmt"

func writeWindowsHTML(content string) error {
	return fmt.Errorf("Windows HTML 剪贴板仅支持 Windows")
}
