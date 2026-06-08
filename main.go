package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"html"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
)

var (
	galleryThemes = []string{
		"warm-card", "fresh-card", "ocean-card",
		"newspaper", "magazine", "ink", "coffee-house",
		"bytedance", "github", "sspai", "midnight",
		"terracotta", "mint-fresh", "sunset-amber", "lavender-dream",
		"sports", "bauhaus", "chinese", "wechat-native",
		"minimal-gold", "focus-blue", "elegant-green", "bold-blue",
	}
)

type Theme map[string]any

type FormatResult struct {
	HTML      string
	Footnotes string
	Title     string
	WordCount int
}

func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "错误:", err)
		os.Exit(1)
	}
}

func firstExistingDir(candidates ...string) string {
	for _, dir := range candidates {
		if dir == "" {
			continue
		}
		if st, err := os.Stat(dir); err == nil && st.IsDir() {
			return dir
		}
	}
	return ""
}

func resolveThemesDir(root string) string {
	return firstExistingDir(
		filepath.Join(root, "themes"),
		filepath.Join(root, "cmd", "format", "themes"),
		filepath.Join(root, "..", "themes"),
		filepath.Join(root, "..", "..", "themes"),
	)
}

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

func loadTheme(root, themeName string) Theme {
	themesDir := resolveThemesDir(root)
	if themesDir == "" {
		fmt.Fprintln(os.Stderr, "错误: 未找到 themes 目录")
		os.Exit(1)
	}
	themePath := filepath.Join(themesDir, themeName+".json")
	if b, err := os.ReadFile(themePath); err == nil {
		var t Theme
		must(json.Unmarshal(b, &t))
		return t
	}

	if strings.Contains(themeName, "-") {
		parts := strings.SplitN(themeName, "-", 2)
		layoutPath := filepath.Join(themesDir, "layouts", parts[0]+".json")
		palettePath := filepath.Join(themesDir, "palettes", parts[1]+".json")
		layoutB, layoutErr := os.ReadFile(layoutPath)
		paletteB, paletteErr := os.ReadFile(palettePath)
		if layoutErr == nil && paletteErr == nil {
			var layout Theme
			var palette map[string]string
			must(json.Unmarshal(layoutB, &layout))
			must(json.Unmarshal(paletteB, &palette))
			return mergeLayoutPalette(layout, palette)
		}
	}

	entries, _ := os.ReadDir(themesDir)
	var names []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".json") {
			names = append(names, strings.TrimSuffix(e.Name(), ".json"))
		}
	}
	sort.Strings(names)
	fmt.Fprintf(os.Stderr, "错误: 主题 '%s' 不存在。可用: %s\n", themeName, strings.Join(names, ", "))
	os.Exit(1)
	return nil
}

func mergeLayoutPalette(layout Theme, palette map[string]string) Theme {
	replacements := map[string]string{
		"{{accent}}":          palette["accent"],
		"{{accent_light}}":    palette["accent_light"],
		"{{primary}}":         palette["primary"],
		"{{background}}":      palette["background"],
		"{{blockquote_bg}}":   palette["blockquote_bg"],
		"{{code_bg}}":         palette["code_bg"],
		"{{hr_color}}":        palette["hr_color"],
		"{{footnote_bg}}":     palette["footnote_bg"],
		"{{table_border}}":    palette["table_border"],
		"{{dark_accent}}":     palette["dark_accent"],
		"{{accent_10}}":       hexToRGBA(palette["accent"], 0.1),
		"{{accent_light_30}}": hexToRGBA(palette["accent_light"], 0.3),
	}
	b, _ := json.Marshal(layout)
	s := string(b)
	for k, v := range replacements {
		s = strings.ReplaceAll(s, k, v)
	}
	var out Theme
	must(json.Unmarshal([]byte(s), &out))
	out["name"] = fmt.Sprintf("%v · %v", layout["name"], palette["name"])
	out["description"] = fmt.Sprintf("%v布局 + %v配色", layout["name"], palette["name"])
	out["colors"] = map[string]string{
		"primary":       palette["primary"],
		"accent":        palette["accent"],
		"background":    palette["background"],
		"blockquote_bg": palette["blockquote_bg"],
		"code_bg":       palette["code_bg"],
		"hr_color":      palette["hr_color"],
		"footnote_bg":   palette["footnote_bg"],
	}
	return out
}

func hexToRGBA(hex string, alpha float64) string {
	if len(hex) != 7 || hex[0] != '#' {
		return "rgba(0,0,0,0.1)"
	}
	r, _ := strconv.ParseInt(hex[1:3], 16, 64)
	g, _ := strconv.ParseInt(hex[3:5], 16, 64)
	b, _ := strconv.ParseInt(hex[5:7], 16, 64)
	return fmt.Sprintf("rgba(%d,%d,%d,%.2f)", r, g, b, alpha)
}

func countWords(text string) int {
	clean := regexp.MustCompile("[#*`\\[\\]()!>|{}_~\\-]").ReplaceAllString(text, "")
	chinese := regexp.MustCompile(`[\p{Han}]`).FindAllString(clean, -1)
	english := regexp.MustCompile(`[a-zA-Z]+`).FindAllString(clean, -1)
	return len(chinese) + len(english)
}

func extractTitle(content string, inputPath string) string {
	if m := regexp.MustCompile(`(?s)^---\n(.*?)\n---`).FindStringSubmatch(content); len(m) > 1 {
		for _, line := range strings.Split(m[1], "\n") {
			if strings.HasPrefix(line, "title:") {
				t := strings.TrimSpace(strings.TrimPrefix(line, "title:"))
				t = strings.Trim(t, `"'`)
				if t != "" {
					return t
				}
			}
		}
	}
	if m := regexp.MustCompile(`(?m)^#\s+(.+)$`).FindStringSubmatch(content); len(m) > 1 {
		return strings.TrimSpace(m[1])
	}
	base := strings.TrimSuffix(filepath.Base(inputPath), filepath.Ext(inputPath))
	base = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}-?`).ReplaceAllString(base, "")
	base = regexp.MustCompile(`-(公众号|小红书|微博)$`).ReplaceAllString(base, "")
	if base == "" {
		return strings.TrimSuffix(filepath.Base(inputPath), filepath.Ext(inputPath))
	}
	return base
}

func stripFrontmatter(content string) string {
	return regexp.MustCompile(`(?s)^---\n.*?\n---\n*`).ReplaceAllString(content, "")
}

func fixCJKSpacing(text string) string {
	lines := strings.Split(text, "\n")
	var out []string
	inCode := false
	cjkLatin := regexp.MustCompile(`([\p{Han}])([a-zA-Z0-9])`)
	latinCjk := regexp.MustCompile(`([a-zA-Z0-9])([\p{Han}])`)
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			inCode = !inCode
			out = append(out, line)
			continue
		}
		if inCode {
			out = append(out, line)
			continue
		}
		protected := []string{}
		protect := func(re *regexp.Regexp, s string) string {
			return re.ReplaceAllStringFunc(s, func(m string) string {
				protected = append(protected, m)
				return fmt.Sprintf("\x00P%d\x00", len(protected)-1)
			})
		}
		line = protect(regexp.MustCompile("`[^`]+`"), line)
		line = protect(regexp.MustCompile(`https?://\S+`), line)
		line = protect(regexp.MustCompile(`!\[[^\]]*\]\([^)]*\)`), line)
		line = protect(regexp.MustCompile(`\[[^\]]*\]\([^)]*\)`), line)
		line = cjkLatin.ReplaceAllString(line, "$1 $2")
		line = latinCjk.ReplaceAllString(line, "$1 $2")
		for i, p := range protected {
			line = strings.ReplaceAll(line, fmt.Sprintf("\x00P%d\x00", i), p)
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

func fixCJKBoldPunctuation(text string) string {
	text = regexp.MustCompile(`\*\*([^*]+?)([，。！？、；："'（）【】《》…—]+)\*\*`).ReplaceAllString(text, "**$1**$2")
	text = regexp.MustCompile(`\*([^*]+?)([，。！？、；："'（）【】《》…—]+)\*`).ReplaceAllString(text, "*$1*$2")
	return text
}

func processCallouts(text string) string {
	lines := strings.Split(text, "\n")
	var out []string
	for i := 0; i < len(lines); {
		m := regexp.MustCompile(`^>\s*\[!([\w]+)\]\s*(.*)`).FindStringSubmatch(lines[i])
		if len(m) == 0 {
			out = append(out, lines[i])
			i++
			continue
		}
		tp := m[1]
		title := strings.TrimSpace(m[2])
		var content []string
		i++
		for i < len(lines) && strings.HasPrefix(lines[i], ">") {
			content = append(content, strings.TrimSpace(strings.TrimPrefix(lines[i], ">")))
			i++
		}
		out = append(out, fmt.Sprintf(`<div class="callout" data-type="%s">`, tp))
		if title != "" {
			out = append(out, fmt.Sprintf(`<p class="callout-title">%s</p>`, title))
		}
		out = append(out, fmt.Sprintf(`<p class="callout-content">%s</p>`, strings.Join(content, "\n")))
		out = append(out, "</div>")
	}
	return strings.Join(out, "\n")
}

func processManualFootnotes(text string) string {
	defs := map[string]string{}
	re := regexp.MustCompile(`(?m)^\[\^(\d+)\]:\s*(.+)$`)
	text = re.ReplaceAllStringFunc(text, func(m string) string {
		g := re.FindStringSubmatch(m)
		defs[g[1]] = strings.TrimSpace(g[2])
		return ""
	})
	if len(defs) == 0 {
		return text
	}
	text = regexp.MustCompile(`\n{3,}`).ReplaceAllString(text, "\n\n")
	text = regexp.MustCompile(`\[\^(\d+)\]`).ReplaceAllString(text, `<sup class="manual-footnote">[$1]</sup>`)
	keys := make([]int, 0, len(defs))
	for k := range defs {
		i, _ := strconv.Atoi(k)
		keys = append(keys, i)
	}
	sort.Ints(keys)
	var b strings.Builder
	b.WriteString("\n<section><p>注释</p>\n")
	for _, k := range keys {
		b.WriteString(fmt.Sprintf("<p>[%d] %s</p>\n", k, defs[strconv.Itoa(k)]))
	}
	b.WriteString("</section>\n")
	return strings.TrimRight(text, "\n") + "\n" + b.String()
}

func processFencedContainers(text string) string {
	re := regexp.MustCompile(`^:::(dialogue|gallery|longimage|stat|timeline|steps|compare|quote)(?:\[([^\]]*)\])?\s*$`)
	lines := strings.Split(text, "\n")
	var out []string
	for i := 0; i < len(lines); {
		m := re.FindStringSubmatch(lines[i])
		if len(m) == 0 {
			out = append(out, lines[i])
			i++
			continue
		}
		tp := m[1]
		title := strings.TrimSpace(m[2])
		i++
		depth := 1
		var body []string
		for i < len(lines) && depth > 0 {
			if re.MatchString(lines[i]) {
				depth++
				body = append(body, lines[i])
			} else if strings.TrimSpace(lines[i]) == ":::" {
				depth--
				if depth > 0 {
					body = append(body, lines[i])
				}
			} else {
				body = append(body, lines[i])
			}
			i++
		}
		inner := processFencedContainers(strings.Join(body, "\n"))
		switch tp {
		case "dialogue":
			out = append(out, buildDialogueHTML(title, strings.Split(inner, "\n")))
		case "gallery":
			out = append(out, fmt.Sprintf(`<section data-container="gallery"><p data-container="gallery-title">%s</p><section data-container="gallery-scroll">%s</section></section>`, title, mdToHTML(inner)))
		case "longimage":
			out = append(out, fmt.Sprintf(`<section data-container="longimage"><p data-container="longimage-title">%s</p><section data-container="longimage-scroll">%s</section></section>`, title, mdToHTML(inner)))
		case "stat":
			out = append(out, buildStatHTML(strings.Split(inner, "\n")))
		case "timeline":
			out = append(out, buildTimelineHTML(title, strings.Split(inner, "\n")))
		case "steps":
			out = append(out, buildStepsHTML(title, strings.Split(inner, "\n")))
		case "compare":
			out = append(out, buildCompareHTML(title, strings.Split(inner, "\n")))
		case "quote":
			out = append(out, buildQuoteHTML(title, strings.Split(inner, "\n")))
		}
	}
	return strings.Join(out, "\n")
}

func buildDialogueHTML(title string, lines []string) string {
	var bubbles []string
	speakers := []string{}
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		m := regexp.MustCompile(`^(.+?)\s*[：:]\s*(.+)$`).FindStringSubmatch(line)
		if len(m) == 0 {
			continue
		}
		speaker, text := strings.TrimSpace(m[1]), strings.TrimSpace(m[2])
		idx := -1
		for i, s := range speakers {
			if s == speaker {
				idx = i
				break
			}
		}
		if idx == -1 {
			speakers = append(speakers, speaker)
			idx = len(speakers) - 1
		}
		side := "left"
		if idx%2 == 1 {
			side = "right"
		}
		bubbles = append(bubbles, fmt.Sprintf(`<section data-container="dialogue-bubble" data-side="%s"><p data-container="dialogue-speaker">%s</p><p data-container="dialogue-text">%s</p></section>`, side, speaker, text))
	}
	return fmt.Sprintf(`<section data-container="dialogue"><p data-container="dialogue-title">%s</p>%s</section>`, title, strings.Join(bubbles, ""))
}

func buildStatHTML(lines []string) string {
	nonEmpty := []string{}
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l != "" {
			nonEmpty = append(nonEmpty, l)
		}
	}
	num, label := "", ""
	if len(nonEmpty) > 0 {
		num = nonEmpty[0]
	}
	if len(nonEmpty) > 1 {
		label = nonEmpty[1]
	}
	return fmt.Sprintf(`<section data-container="stat"><p data-container="stat-number">%s</p><p data-container="stat-label">%s</p></section>`, num, label)
}

func buildTimelineHTML(title string, lines []string) string {
	var b strings.Builder
	b.WriteString(`<section data-container="timeline">`)
	if title != "" {
		b.WriteString(fmt.Sprintf(`<p data-container="timeline-title">%s</p>`, title))
	}
	re := regexp.MustCompile(`^(.+?)\s*[：:]\s*(.+)$`)
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		m := re.FindStringSubmatch(line)
		if len(m) == 0 {
			continue
		}
		b.WriteString(fmt.Sprintf(`<section data-container="timeline-item"><span data-container="timeline-time">%s</span><span data-container="timeline-dot">●</span><span data-container="timeline-content">%s</span></section>`, strings.TrimSpace(m[1]), strings.TrimSpace(m[2])))
	}
	b.WriteString(`</section>`)
	return b.String()
}

func buildStepsHTML(title string, lines []string) string {
	var b strings.Builder
	b.WriteString(`<section data-container="steps">`)
	if title != "" {
		b.WriteString(fmt.Sprintf(`<p data-container="steps-title">%s</p>`, title))
	}
	n := 0
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		n++
		b.WriteString(fmt.Sprintf(`<section data-container="steps-item"><span data-container="steps-number">%d</span><span data-container="steps-content">%s</span></section>`, n, line))
	}
	b.WriteString(`</section>`)
	return b.String()
}

func buildCompareHTML(title string, lines []string) string {
	leftName, rightName := "", ""
	if strings.Contains(title, " vs ") {
		parts := strings.SplitN(title, " vs ", 2)
		leftName, rightName = strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
	}
	if strings.Contains(title, " VS ") {
		parts := strings.SplitN(title, " VS ", 2)
		leftName, rightName = strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
	}
	var b strings.Builder
	b.WriteString(`<section data-container="compare">`)
	if leftName != "" || rightName != "" {
		b.WriteString(fmt.Sprintf(`<section data-container="compare-header"><span data-container="compare-header-left">%s</span><span data-container="compare-header-right">%s</span></section>`, leftName, rightName))
	}
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		left, right := line, ""
		if strings.Contains(line, "|") {
			parts := strings.SplitN(line, "|", 2)
			left = strings.TrimSpace(parts[0])
			right = strings.TrimSpace(parts[1])
		}
		b.WriteString(fmt.Sprintf(`<section data-container="compare-row"><span data-container="compare-left">%s</span><span data-container="compare-right">%s</span></section>`, left, right))
	}
	b.WriteString(`</section>`)
	return b.String()
}

func buildQuoteHTML(author string, lines []string) string {
	parts := []string{}
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l != "" {
			parts = append(parts, l)
		}
	}
	return fmt.Sprintf(`<section data-container="quote-card"><p data-container="quote-mark">❝</p><p data-container="quote-text">%s</p><p data-container="quote-author">— %s</p></section>`, strings.Join(parts, "<br>"), author)
}

func mdToHTML(content string) string {
	var buf bytes.Buffer
	md := goldmark.New(
		goldmark.WithExtensions(extension.GFM, extension.Table, extension.Strikethrough),
		goldmark.WithParserOptions(parser.WithAutoHeadingID()),
	)
	if err := md.Convert([]byte(content), &buf); err != nil {
		return html.EscapeString(content)
	}
	return buf.String()
}

func convertWikilinks(text, vaultRoot, outputDir string, searchPaths []string) string {
	imagesDir := filepath.Join(outputDir, "images")
	roots := []string{vaultRoot}
	roots = append(roots, searchPaths...)
	re := regexp.MustCompile(`!\[\[([^\]]+)\]\]`)
	return re.ReplaceAllStringFunc(text, func(m string) string {
		g := re.FindStringSubmatch(m)
		filename := strings.TrimSpace(g[1])
		if strings.Contains(filename, "|") {
			filename = strings.TrimSpace(strings.SplitN(filename, "|", 2)[0])
		}
		for _, r := range roots {
			r = ExpandPath(r)
			if r == "" {
				continue
			}
			var found string
			filepath.WalkDir(r, func(path string, d os.DirEntry, err error) error {
				if err != nil || d.IsDir() || found != "" {
					return nil
				}
				if d.Name() == filename {
					found = path
				}
				return nil
			})
			if found != "" {
				_ = os.MkdirAll(imagesDir, 0o755)
				dest := filepath.Join(imagesDir, filename)
				if _, err := os.Stat(dest); os.IsNotExist(err) {
					if b, err := os.ReadFile(found); err == nil {
						_ = os.WriteFile(dest, b, 0o644)
					}
				}
				return fmt.Sprintf(`<section data-role="img-wrapper"><img src="images/%s" alt="%s"></section>`, filename, filename)
			}
		}
		return fmt.Sprintf(`<span style="color:#999;">[图片: %s]</span>`, filename)
	})
}

func copyMarkdownImages(text, inputDir, outputDir string) string {
	imagesDir := filepath.Join(outputDir, "images")
	re := regexp.MustCompile(`!\[([^\]]*)\]\(([^)]+)\)`)
	return re.ReplaceAllStringFunc(text, func(m string) string {
		g := re.FindStringSubmatch(m)
		alt, src := g[1], strings.TrimSpace(g[2])
		if strings.HasPrefix(src, "http://") || strings.HasPrefix(src, "https://") {
			return m
		}
		p := filepath.Clean(filepath.Join(inputDir, src))
		if _, err := os.Stat(p); err != nil {
			return m
		}
		_ = os.MkdirAll(imagesDir, 0o755)
		dest := filepath.Join(imagesDir, filepath.Base(p))
		if _, err := os.Stat(dest); os.IsNotExist(err) {
			if b, err := os.ReadFile(p); err == nil {
				_ = os.WriteFile(dest, b, 0o644)
			}
		}
		return fmt.Sprintf("![%s](images/%s)", alt, filepath.Base(p))
	})
}

func extractLinksAsFootnotes(raw string) (string, string) {
	re := regexp.MustCompile(`<a[^>]*href="([^"]*)"[^>]*>(.*?)</a>`)
	footnotes := []string{}
	counter := 0
	processed := re.ReplaceAllStringFunc(raw, func(m string) string {
		g := re.FindStringSubmatch(m)
		href := g[1]
		text := g[2]
		if !strings.HasPrefix(href, "http") {
			return m
		}
		counter++
		footnotes = append(footnotes, fmt.Sprintf("<p>[%d] %s: %s</p>", counter, text, href))
		return fmt.Sprintf(`%s<sup>[%d]</sup>`, text, counter)
	})
	if len(footnotes) == 0 {
		return processed, ""
	}
	fn := "<section><p>参考链接</p>\n" + strings.Join(footnotes, "\n") + "\n</section>"
	return processed, fn
}

func buildStyleString(props map[string]any) string {
	parts := []string{}
	for k, v := range props {
		parts = append(parts, strings.ReplaceAll(k, "_", "-")+":"+fmt.Sprint(v))
	}
	sort.Strings(parts)
	return strings.Join(parts, ";")
}

func toMapAny(v any) map[string]any {
	if v == nil {
		return map[string]any{}
	}
	m, ok := v.(map[string]any)
	if !ok {
		return map[string]any{}
	}
	return m
}

func injectInlineStyles(raw string, theme Theme, skipWrapper bool) string {
	styles := toMapAny(theme["styles"])
	styleMap := map[string]string{}
	for k, v := range styles {
		styleMap[k] = buildStyleString(toMapAny(v))
	}

	html := raw
	simpleTags := []string{"h1", "h2", "h3", "h4", "h5", "h6", "p", "strong", "em", "a", "img", "hr", "code", "table", "th", "td"}
	for _, tag := range simpleTags {
		s, ok := styleMap[tag]
		if !ok || s == "" {
			continue
		}
		if tag == "hr" {
			html = regexp.MustCompile(`<hr\s*/?>`).ReplaceAllString(html, fmt.Sprintf(`<hr style="%s">`, s))
			continue
		}
		if tag == "img" {
			imgStyle := s
			if !strings.Contains(imgStyle, "width") {
				imgStyle += ";width:100%"
			}
			html = regexp.MustCompile(`<img[^>]*>`).ReplaceAllStringFunc(html, func(m string) string {
				if strings.Contains(m, ` style="`) {
					return m
				}
				return strings.Replace(m, "<img", fmt.Sprintf(`<img style="%s"`, imgStyle), 1)
			})
			continue
		}
		html = regexp.MustCompile(fmt.Sprintf(`<%s>`, tag)).ReplaceAllString(html, fmt.Sprintf(`<%s style="%s">`, tag, s))
	}

	if s := styleMap["wrapper"]; s != "" && !skipWrapper {
		html = `<section style="` + s + `">` + html + `</section>`
	}
	return html
}

func convertImageCaptions(raw string) string {
	style := `text-align:center;font-size:13px;color:#999999;margin-top:-8px;margin-bottom:16px;font-style:normal`
	raw = regexp.MustCompile(`(</section>\s*)<p[^>]*><em>(.*?)</em></p>`).ReplaceAllString(raw, `$1<p style="`+style+`">$2</p>`)
	raw = regexp.MustCompile(`(</p>\s*)<p[^>]*><em>(.*?)</em></p>`).ReplaceAllString(raw, `$1<p style="`+style+`">$2</p>`)
	return raw
}

func formatForOutput(content, inputPath string, theme Theme, outputDir, vaultRoot string, cfg *Config, outputFormat string) FormatResult {
	title := extractTitle(content, inputPath)
	wordCount := countWords(content)
	content = stripFrontmatter(content)
	content = fixCJKSpacing(content)
	content = fixCJKBoldPunctuation(content)
	content = processCallouts(content)
	content = processManualFootnotes(content)
	content = processFencedContainers(content)
	content = regexp.MustCompile(`~~(.+?)~~`).ReplaceAllString(content, "<del>$1</del>")
	_ = os.MkdirAll(outputDir, 0o755)
	content = convertWikilinks(content, vaultRoot, outputDir, cfg.ImageSearchPaths)
	content = copyMarkdownImages(content, filepath.Dir(inputPath), outputDir)
	htmlContent := mdToHTML(content)
	if outputFormat == "plain" {
		return FormatResult{HTML: htmlContent, Title: title, WordCount: wordCount}
	}
	htmlContent, foot := extractLinksAsFootnotes(htmlContent)
	if outputFormat == "html" {
		return FormatResult{HTML: htmlContent, Footnotes: foot, Title: title, WordCount: wordCount}
	}
	htmlContent = injectInlineStyles(htmlContent, theme, false)
	if foot != "" {
		foot = injectInlineStyles(foot, theme, true)
	}
	htmlContent = convertImageCaptions(htmlContent)
	if foot != "" {
		foot = convertImageCaptions(foot)
	}
	return FormatResult{HTML: htmlContent, Footnotes: foot, Title: title, WordCount: wordCount}
}

func generatePreview(root string, articleHTML string, footnote string, theme Theme, title string, wc int, out string) error {
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
	tpl = strings.ReplaceAll(tpl, "{{THEME_NAME}}", fmt.Sprint(theme["name"]))
	tpl = strings.ReplaceAll(tpl, "{{WORD_COUNT}}", fmt.Sprintf("%d", wc))
	tpl = strings.ReplaceAll(tpl, "{{ARTICLE_HTML}}", full)
	_ = os.MkdirAll(filepath.Dir(out), 0o755)
	return os.WriteFile(out, []byte(tpl), 0o644)
}

func generateGallery(root string, rendered map[string]string, themeMap map[string]Theme, themeIDs []string, title string, wc int, outDir string, recommended map[string]bool) (string, error) {
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

func maybeOpenBrowser(path string) {
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

func readClipboard() (string, error) {
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

func writeClipboard(content string) error {
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

func writeHTMLClipboard(content string) error {
	switch runtime.GOOS {
	case "windows":
		return writeWindowsHTMLClipboard(content)
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
		return writeClipboard(content)
	}
}

func main() {
	input := flag.String("i", "", "输入 Markdown 文件路径")
	themeName := flag.String("t", "", "主题名称")
	vaultRoot := flag.String("vault-root", "", "Obsidian Vault 根目录")
	output := flag.String("o", "", "输出目录")
	openBrowser := flag.Bool("w", false, "自动打开浏览器")
	std := flag.Bool("std", false, "从标准输入读取，并在不打开浏览器时输出到标准输出")
	gallery := flag.Bool("gallery", false, "主题画廊模式")
	recommend := flag.String("recommend", "", "推荐主题ID，用逗号分隔")
	formatType := flag.String("format", "wechat", "输出格式: wechat/html/plain")
	flag.Parse()

	root, _ := os.Getwd()
	cfg, err := LoadConfig(root)
	must(err)
	if *themeName == "" {
		*themeName = cfg.Settings.DefaultTheme
	}
	if *output == "" {
		*output = cfg.OutputDir
	}
	if *vaultRoot == "" {
		*vaultRoot = cfg.VaultRoot
	}
	themesDir := resolveThemesDir(root)
	if themesDir == "" {
		fmt.Fprintln(os.Stderr, "错误: 未找到 themes 目录")
		os.Exit(1)
	}
	theme := loadTheme(root, *themeName)
	in := *input
	content := ""
	if in != "" {
		if _, err := os.Stat(in); err != nil {
			fmt.Fprintln(os.Stderr, "错误: 文件不存在 -", in)
			os.Exit(1)
		}
		contentBytes, err := os.ReadFile(in)
		must(err)
		content = string(contentBytes)
	} else if *std {
		contentBytes, err := io.ReadAll(os.Stdin)
		must(err)
		content = string(contentBytes)
		in = "stdin.md"
	} else {
		clipboardContent, err := readClipboard()
		if err != nil {
			fmt.Fprintln(os.Stderr, "错误: 读取剪贴板失败 -", err)
			os.Exit(1)
		}
		content = clipboardContent
		in = "clipboard.md"
	}
	if strings.TrimSpace(content) == "" {
		fmt.Fprintln(os.Stderr, "错误: 输入内容为空")
		os.Exit(1)
	}
	fileStem := regexp.MustCompile(`-(公众号|小红书|微博)$`).ReplaceAllString(strings.TrimSuffix(filepath.Base(in), filepath.Ext(in)), "")
	outDir := filepath.Join(*output, fileStem)

	logOut := io.Writer(os.Stdout)
	if *std && !*openBrowser && *formatType == "wechat" && !*gallery {
		logOut = os.Stderr
	}
	fmt.Fprintf(logOut, "主题: %v (%s)\n", theme["name"], *themeName)
	fmt.Fprintln(logOut, "输入:", in)
	fmt.Fprintln(logOut, "标题:", extractTitle(content, in))
	fmt.Fprintf(logOut, "字数: %d\n", countWords(content))

	if *gallery {
		base := formatForOutput(content, in, theme, outDir, *vaultRoot, cfg, "html")
		themeMap := map[string]Theme{}
		themeIDs := []string{}
		for _, tid := range galleryThemes {
			p := filepath.Join(themesDir, tid+".json")
			if b, err := os.ReadFile(p); err == nil {
				var t Theme
				_ = json.Unmarshal(b, &t)
				themeMap[tid] = t
				themeIDs = append(themeIDs, tid)
			}
		}
		fmt.Fprintf(logOut, "\n画廊模式: 并行渲染 %d 个主题...\n", len(themeIDs))
		rendered := map[string]string{}
		var mu sync.Mutex
		wg := sync.WaitGroup{}
		sem := make(chan struct{}, 8)
		for _, tid := range themeIDs {
			wg.Add(1)
			sem <- struct{}{}
			go func(id string) {
				defer wg.Done()
				defer func() { <-sem }()
				h := injectInlineStyles(base.HTML, themeMap[id], false)
				h = convertImageCaptions(h)
				if base.Footnotes != "" {
					fn := injectInlineStyles(base.Footnotes, themeMap[id], true)
					fn = convertImageCaptions(fn)
					h += "\n" + fn
				}
				mu.Lock()
				rendered[id] = h
				mu.Unlock()
				fmt.Fprintf(logOut, "  ✓ %v (%s)\n", themeMap[id]["name"], id)
			}(tid)
		}
		wg.Wait()
		recommended := map[string]bool{}
		for _, s := range strings.Split(*recommend, ",") {
			s = strings.TrimSpace(s)
			if s != "" {
				recommended[s] = true
			}
		}
		p, err := generateGallery(root, rendered, themeMap, themeIDs, base.Title, base.WordCount, outDir, recommended)
		must(err)
		fmt.Fprintln(logOut, "\n画廊页面:", p)
		if cfg.Settings.AutoOpenBrowser && *openBrowser {
			maybeOpenBrowser(p)
			fmt.Fprintln(logOut, "已在浏览器中打开画廊")
		}
		fmt.Fprintln(logOut, "\n完成! 选中主题后点「用这个风格排版」即可复制到剪贴板。")
		return
	}

	result := formatForOutput(content, in, theme, outDir, *vaultRoot, cfg, *formatType)
	_ = os.MkdirAll(outDir, 0o755)
	if *formatType != "wechat" {
		outPath := filepath.Join(outDir, "article."+*formatType+".html")
		outHTML := result.HTML
		if result.Footnotes != "" {
			outHTML += "\n" + result.Footnotes
		}
		must(os.WriteFile(outPath, []byte(outHTML), 0o644))
		fmt.Fprintln(logOut, "\n输出:", outPath)
		return
	}
	full := result.HTML
	if result.Footnotes != "" {
		full += "\n" + result.Footnotes
	}
	if !*openBrowser {
		if *std {
			fmt.Print(full)
		} else {
			must(writeHTMLClipboard(full))
			fmt.Fprintln(logOut, "\n已复制微信富文本排版内容到剪贴板")
		}
		fmt.Fprintln(logOut, "\n完成! 可直接粘贴到公众号后台。")
		return
	}
	articlePath := filepath.Join(outDir, "article.html")
	must(os.WriteFile(articlePath, []byte(full), 0o644))
	previewPath := filepath.Join(outDir, "preview.html")
	must(generatePreview(root, result.HTML, result.Footnotes, theme, result.Title, result.WordCount, previewPath))
	fmt.Fprintln(logOut, "\n排版成品:", previewPath)
	if cfg.Settings.AutoOpenBrowser {
		maybeOpenBrowser(previewPath)
		fmt.Fprintln(logOut, "已在浏览器中打开预览")
	}
	fmt.Fprintln(logOut, "\n完成! 在浏览器中点击「复制到微信」按钮，然后粘贴到公众号后台即可。")
}
