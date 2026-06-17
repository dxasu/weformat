package output

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"weformat/internal/theme"
)

func resolveTemplateFile(root string, name string) string {
	candidates := []string{
		filepath.Join(root, "templates", name),
		filepath.Join(root, "..", "templates", name),
		filepath.Join(root, "..", "..", "templates", name),
		filepath.Join(root, "cmd", "format", "templates", name),
	}
	for _, p := range candidates {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	return ""
}

func GeneratePreview(root string, articleHTML string, footnote string, th theme.Theme, title string, wc int, out string) error {
	tplPath := resolveTemplateFile(root, "preview.html")
	if tplPath == "" {
		return fmt.Errorf("未找到模板文件 preview.html")
	}
	b, err := os.ReadFile(tplPath)
	if err != nil {
		return err
	}
	full := articleHTML
	if footnote != "" {
		full += "\n" + footnote
	}
	tpl := string(b)
	tpl = strings.ReplaceAll(tpl, "{{TITLE}}", title)
	tpl = strings.ReplaceAll(tpl, "{{THEME_NAME}}", fmt.Sprint(th["name"]))
	tpl = strings.ReplaceAll(tpl, "{{WORD_COUNT}}", fmt.Sprintf("%d", wc))
	tpl = strings.ReplaceAll(tpl, "{{ARTICLE_HTML}}", full)
	_ = os.MkdirAll(filepath.Dir(out), 0o755)
	return os.WriteFile(out, []byte(tpl), 0o644)
}

func GenerateGallery(root string, rendered map[string]string, themeMap map[string]theme.Theme, themeIDs []string, title string, wc int, outDir string, recommended map[string]bool) (string, error) {
	tplPath := resolveTemplateFile(root, "gallery.html")
	if tplPath == "" {
		return "", fmt.Errorf("未找到模板文件 gallery.html")
	}
	b, err := os.ReadFile(tplPath)
	if err != nil {
		return "", err
	}
	defaultTheme := ""
	if len(themeIDs) > 0 {
		defaultTheme = themeIDs[0]
	}
	var buttons strings.Builder
	for i, tid := range themeIDs {
		active := ""
		if i == 0 {
			active = " active"
		}
		rec := ""
		recBadge := ""
		if recommended[tid] {
			rec = " recommended"
			recBadge = `<span class="rec-badge">推荐</span>`
		}
		name := fmt.Sprint(themeMap[tid]["name"])
		accent := "#333"
		if colors, ok := themeMap[tid]["colors"].(map[string]any); ok {
			if v, ok := colors["accent"]; ok {
				accent = fmt.Sprint(v)
			}
		}
		buttons.WriteString(fmt.Sprintf(`<button class="theme-btn%s%s" data-theme="%s" onclick="switchTheme('%s')"><span class="theme-dot" style="background:%s"></span>%s%s</button>`, active, rec, tid, tid, accent, name, recBadge))
	}
	var previews strings.Builder
	for i, tid := range themeIDs {
		display := "none"
		if i == 0 {
			display = "block"
		}
		previews.WriteString(fmt.Sprintf(`<div class="theme-preview" data-theme="%s" style="display:%s">%s</div>`, tid, display, rendered[tid]))
	}
	out := string(b)
	out = strings.ReplaceAll(out, "{{TITLE}}", title)
	out = strings.ReplaceAll(out, "{{WORD_COUNT}}", fmt.Sprintf("%d", wc))
	out = strings.ReplaceAll(out, "{{THEME_BUTTONS}}", buttons.String())
	out = strings.ReplaceAll(out, "{{THEME_PREVIEWS}}", previews.String())
	out = strings.ReplaceAll(out, "{{DEFAULT_THEME}}", defaultTheme)
	_ = os.MkdirAll(filepath.Dir(filepath.Join("/tmp/wechat-format", "selected-theme.txt")), 0o755)
	_ = os.WriteFile(filepath.Join("/tmp/wechat-format", "selected-theme.txt"), []byte(defaultTheme), 0o644)
	galleryPath := filepath.Join(outDir, "gallery.html")
	_ = os.MkdirAll(outDir, 0o755)
	if err := os.WriteFile(galleryPath, []byte(out), 0o644); err != nil {
		return "", err
	}
	return galleryPath, nil
}

func MaybeOpenBrowser(path string) {
	url := "file://" + filepath.ToSlash(path)
	switch runtime.GOOS {
	case "windows":
		_ = exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	case "darwin":
		_ = exec.Command("open", url).Start()
	default:
		_ = exec.Command("xdg-open", url).Start()
	}
}
