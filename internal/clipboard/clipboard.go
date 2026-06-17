package clipboard

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

func Read() (string, error) {
	switch runtime.GOOS {
	case "windows":
		out, err := exec.Command("powershell", "-NoProfile", "-Command", "[Console]::OutputEncoding = [Text.UTF8Encoding]::UTF8; Get-Clipboard -Raw").Output()
		return string(out), err
	case "darwin":
		out, err := exec.Command("pbpaste").Output()
		return string(out), err
	default:
		if out, err := exec.Command("wl-paste", "--no-newline").Output(); err == nil {
			return string(out), nil
		}
		out, err := exec.Command("xclip", "-selection", "clipboard", "-o").Output()
		return string(out), err
	}
}

func Write(content string) error {
	switch runtime.GOOS {
	case "windows":
		cmd := exec.Command("powershell", "-NoProfile", "-Command", "[Console]::InputEncoding = [Text.UTF8Encoding]::UTF8; Set-Clipboard -Value ([Console]::In.ReadToEnd())")
		cmd.Stdin = strings.NewReader(content)
		return cmd.Run()
	case "darwin":
		cmd := exec.Command("pbcopy")
		cmd.Stdin = strings.NewReader(content)
		return cmd.Run()
	default:
		if _, err := exec.LookPath("wl-copy"); err == nil {
			cmd := exec.Command("wl-copy")
			cmd.Stdin = strings.NewReader(content)
			return cmd.Run()
		}
		cmd := exec.Command("xclip", "-selection", "clipboard")
		cmd.Stdin = strings.NewReader(content)
		return cmd.Run()
	}
}

func buildCFHTML(fragment string) string {
	header := "Version:0.9\r\n" +
		"StartHTML:0000000000\r\n" +
		"EndHTML:0000000000\r\n" +
		"StartFragment:0000000000\r\n" +
		"EndFragment:0000000000\r\n"
	prefix := "<!DOCTYPE html><html><body><!--StartFragment-->"
	suffix := "<!--EndFragment--></body></html>"
	startHTML := len([]byte(header))
	startFragment := startHTML + len([]byte(prefix))
	endFragment := startFragment + len([]byte(fragment))
	endHTML := startHTML + len([]byte(prefix+fragment+suffix))
	header = fmt.Sprintf("Version:0.9\r\nStartHTML:%010d\r\nEndHTML:%010d\r\nStartFragment:%010d\r\nEndFragment:%010d\r\n", startHTML, endHTML, startFragment, endFragment)
	return header + prefix + fragment + suffix
}

func WriteHTML(content string) error {
	switch runtime.GOOS {
	case "windows":
		return writeWindowsHTML(content)
	case "linux":
		if _, err := exec.LookPath("wl-copy"); err == nil {
			cmd := exec.Command("wl-copy", "--type", "text/html")
			cmd.Stdin = strings.NewReader(content)
			return cmd.Run()
		}
		cmd := exec.Command("xclip", "-selection", "clipboard", "-t", "text/html")
		cmd.Stdin = strings.NewReader(content)
		return cmd.Run()
	default:
		return Write(content)
	}
}
