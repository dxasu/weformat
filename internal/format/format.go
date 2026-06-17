package format

import (
	"bytes"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"weformat/internal/config"
	"weformat/internal/theme"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
)

type Result struct {
	HTML      string
	Footnotes string
	Title     string
	WordCount int
}

func CountWords(text string) int {
	clean := regexp.MustCompile("[#*`\\[\\]()!>|{}_~\\-]").ReplaceAllString(text, "")
	chinese := regexp.MustCompile(`[\p{Han}]`).FindAllString(clean, -1)
	english := regexp.MustCompile(`[a-zA-Z]+`).FindAllString(clean, -1)
	return len(chinese) + len(english)
}

func ExtractTitle(content string, inputPath string) string {
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
			r = config.ExpandPath(r)
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

func buildH2ImageHTML(th theme.Theme) string {
	cfg := toMapAny(th["h2_image"])
	src := strings.TrimSpace(fmt.Sprint(cfg["src"]))
	if src == "" || src == "<nil>" {
		return ""
	}

	alt := ""
	if v, ok := cfg["alt"]; ok {
		alt = fmt.Sprint(v)
	}
	wrapperStyle := buildStyleString(toMapAny(cfg["wrapper_style"]))
	if wrapperStyle == "" {
		wrapperStyle = "margin:0 0 12px;padding:0;text-align:center"
	}
	imageStyle := buildStyleString(toMapAny(cfg["image_style"]))
	if imageStyle == "" {
		imageStyle = "width:60px !important;height:auto !important;vertical-align:bottom"
	}

	return fmt.Sprintf(`<h2 data-role="h2-image" style="%s"><span><img src="%s" alt="%s" style="%s"></span></h2>`,
		html.EscapeString(wrapperStyle),
		html.EscapeString(src),
		html.EscapeString(alt),
		html.EscapeString(imageStyle),
	)
}

func InjectInlineStyles(raw string, th theme.Theme, skipWrapper bool) string {
	styles := toMapAny(th["styles"])
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
		html = regexp.MustCompile(fmt.Sprintf(`<%s(\s[^>]*)?>`, tag)).ReplaceAllStringFunc(html, func(m string) string {
			if strings.Contains(m, ` style="`) {
				return m
			}
			return strings.Replace(m, "<"+tag, fmt.Sprintf(`<%s style="%s"`, tag, s), 1)
		})
	}

	if !skipWrapper {
		if h2Image := buildH2ImageHTML(th); h2Image != "" {
			html = regexp.MustCompile(`<h2\b`).ReplaceAllString(html, h2Image+`<h2`)
		}
	}

	if s := styleMap["wrapper"]; s != "" && !skipWrapper {
		html = `<section style="` + s + `">` + html + `</section>`
	}
	return html
}

func ConvertImageCaptions(raw string) string {
	style := `text-align:center;font-size:13px;color:#999999;margin-top:-8px;margin-bottom:16px;font-style:normal`
	raw = regexp.MustCompile(`(</section>\s*)<p[^>]*><em>(.*?)</em></p>`).ReplaceAllString(raw, `$1<p style="`+style+`">$2</p>`)
	raw = regexp.MustCompile(`(</p>\s*)<p[^>]*><em>(.*?)</em></p>`).ReplaceAllString(raw, `$1<p style="`+style+`">$2</p>`)
	return raw
}

func ForOutput(content, inputPath string, th theme.Theme, outputDir, vaultRoot string, cfg *config.Config, outputFormat string) Result {
	title := ExtractTitle(content, inputPath)
	wordCount := CountWords(content)
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
		return Result{HTML: htmlContent, Title: title, WordCount: wordCount}
	}
	htmlContent, foot := extractLinksAsFootnotes(htmlContent)
	if outputFormat == "html" {
		return Result{HTML: htmlContent, Footnotes: foot, Title: title, WordCount: wordCount}
	}
	htmlContent = InjectInlineStyles(htmlContent, th, false)
	if foot != "" {
		foot = InjectInlineStyles(foot, th, true)
	}
	htmlContent = ConvertImageCaptions(htmlContent)
	if foot != "" {
		foot = ConvertImageCaptions(foot)
	}
	return Result{HTML: htmlContent, Footnotes: foot, Title: title, WordCount: wordCount}
}
