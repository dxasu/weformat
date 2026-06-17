package app

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"weformat/internal/clipboard"
	"weformat/internal/config"
	"weformat/internal/format"
	"weformat/internal/output"
	"weformat/internal/theme"
)

func Run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("weformat", flag.ContinueOnError)
	fs.SetOutput(stderr)

	input := fs.String("i", "", "输入 Markdown 文件路径")
	themeName := fs.String("t", "core", "主题名称")
	vaultRoot := fs.String("vault-root", "", "Obsidian Vault 根目录")
	gallery := fs.Bool("gallery", false, "主题画廊模式")
	recommend := fs.String("recommend", "", "推荐主题ID，用逗号分隔")
	formatType := fs.String("format", "wechat", "输出格式: wechat/html/plain")
	outputDir := fs.String("o", "", "输出目录")
	openBrowser := fs.Bool("w", false, "自动打开浏览器")
	useClipboard := fs.Bool("c", true, "复制结果到剪贴板")
	std := fs.Bool("std", false, "从标准输入读取，输出到标准输出")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	root, _ := os.Getwd()
	cfg, err := config.Load(root)
	if err != nil {
		fmt.Fprintln(stderr, "错误:", err)
		return 1
	}
	if !*openBrowser && !*useClipboard && !*std {
		fmt.Fprintln(stderr, "错误: 请使用 -w 或 -c 或 -std 参数")
		return 1
	}
	if *openBrowser {
		*useClipboard = false
	}
	if *themeName == "" {
		*themeName = cfg.Settings.DefaultTheme
	}
	if *outputDir == "" {
		*outputDir = cfg.OutputDir
	}
	if *vaultRoot == "" {
		*vaultRoot = cfg.VaultRoot
	}
	themesDir := theme.ResolveThemesDir(root)
	if themesDir == "" {
		fmt.Fprintln(stderr, "错误: 未找到 themes 目录")
		return 1
	}
	th, err := theme.Load(root, *themeName)
	if err != nil {
		fmt.Fprintln(stderr, "错误:", err)
		return 1
	}

	in := *input
	positional := fs.Args()
	if in != "" && len(positional) > 0 {
		fmt.Fprintln(stderr, "错误: 请不要同时使用 -i 和位置输入文件")
		return 1
	}
	if in == "" && len(positional) > 0 {
		if len(positional) > 1 {
			fmt.Fprintln(stderr, "错误: 只能指定一个输入文件")
			return 1
		}
		in = positional[0]
	}

	content, inputName, ok := readInput(in, *std, stdin, stderr)
	if !ok {
		return 1
	}
	in = inputName
	if strings.TrimSpace(content) == "" {
		fmt.Fprintln(stderr, "错误: 输入内容为空")
		return 1
	}
	fileStem := regexp.MustCompile(`-(公众号|小红书|微博)$`).ReplaceAllString(strings.TrimSuffix(filepath.Base(in), filepath.Ext(in)), "")
	outDir := filepath.Join(*outputDir, fileStem)

	logOut := stdout
	if *std && !*openBrowser && *formatType == "wechat" && !*gallery {
		logOut = stderr
	}
	fmt.Fprintf(logOut, "主题: %v (%s)\n", th["name"], *themeName)
	fmt.Fprintln(logOut, "输入:", in)
	fmt.Fprintln(logOut, "标题:", format.ExtractTitle(content, in))
	fmt.Fprintf(logOut, "字数: %d\n", format.CountWords(content))

	if *gallery {
		return runGallery(root, themesDir, content, in, th, outDir, *vaultRoot, cfg, *recommend, *openBrowser, logOut, stderr)
	}

	result := format.ForOutput(content, in, th, outDir, *vaultRoot, cfg, *formatType)
	_ = os.MkdirAll(outDir, 0o755)
	if *formatType != "wechat" {
		outPath := filepath.Join(outDir, "article."+*formatType+".html")
		outHTML := result.HTML
		if result.Footnotes != "" {
			outHTML += "\n" + result.Footnotes
		}
		if err := os.WriteFile(outPath, []byte(outHTML), 0o644); err != nil {
			fmt.Fprintln(stderr, "错误:", err)
			return 1
		}
		fmt.Fprintln(logOut, "\n输出:", outPath)
		return 0
	}

	full := result.HTML
	if result.Footnotes != "" {
		full += "\n" + result.Footnotes
	}
	if *std {
		fmt.Fprint(stdout, full)
	}
	if *useClipboard {
		if err := clipboard.WriteHTML(full); err != nil {
			fmt.Fprintln(stderr, "错误:", err)
			return 1
		}
		fmt.Fprintln(logOut, "\n已复制微信富文本排版内容到剪贴板")
	}
	if *openBrowser {
		articlePath := filepath.Join(outDir, "article.html")
		if err := os.WriteFile(articlePath, []byte(full), 0o644); err != nil {
			fmt.Fprintln(stderr, "错误:", err)
			return 1
		}
		previewPath := filepath.Join(outDir, "preview.html")
		if err := output.GeneratePreview(root, result.HTML, result.Footnotes, th, result.Title, result.WordCount, previewPath); err != nil {
			fmt.Fprintln(stderr, "错误:", err)
			return 1
		}
		fmt.Fprintln(logOut, "\n排版成品:", previewPath)
		if cfg.Settings.AutoOpenBrowser {
			output.MaybeOpenBrowser(previewPath)
			fmt.Fprintln(logOut, "已在浏览器中打开预览")
		}
		fmt.Fprintln(logOut, "\n完成! 在浏览器中点击「复制到微信」按钮，然后粘贴到公众号后台即可。")
	}
	return 0
}

func readInput(inputPath string, std bool, stdin io.Reader, stderr io.Writer) (string, string, bool) {
	if inputPath != "" {
		if _, err := os.Stat(inputPath); err != nil {
			fmt.Fprintln(stderr, "错误: 文件不存在 -", inputPath)
			return "", "", false
		}
		contentBytes, err := os.ReadFile(inputPath)
		if err != nil {
			fmt.Fprintln(stderr, "错误:", err)
			return "", "", false
		}
		return string(contentBytes), inputPath, true
	}
	if std {
		contentBytes, err := io.ReadAll(stdin)
		if err != nil {
			fmt.Fprintln(stderr, "错误:", err)
			return "", "", false
		}
		return string(contentBytes), "stdin.md", true
	}
	clipboardContent, err := clipboard.Read()
	if err != nil {
		fmt.Fprintln(stderr, "错误: 读取剪贴板失败 -", err)
		return "", "", false
	}
	return clipboardContent, "clipboard.md", true
}

func runGallery(root, themesDir, content, inputPath string, baseTheme theme.Theme, outDir, vaultRoot string, cfg *config.Config, recommend string, openBrowser bool, logOut, stderr io.Writer) int {
	base := format.ForOutput(content, inputPath, baseTheme, outDir, vaultRoot, cfg, "html")
	themeMap := map[string]theme.Theme{}
	themeIDs := []string{}
	for _, tid := range theme.GalleryThemes {
		p := filepath.Join(themesDir, tid+".json")
		if b, err := os.ReadFile(p); err == nil {
			var t theme.Theme
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
			h := format.InjectInlineStyles(base.HTML, themeMap[id], false)
			h = format.ConvertImageCaptions(h)
			if base.Footnotes != "" {
				fn := format.InjectInlineStyles(base.Footnotes, themeMap[id], true)
				fn = format.ConvertImageCaptions(fn)
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
	for _, s := range strings.Split(recommend, ",") {
		s = strings.TrimSpace(s)
		if s != "" {
			recommended[s] = true
		}
	}
	p, err := output.GenerateGallery(root, rendered, themeMap, themeIDs, base.Title, base.WordCount, outDir, recommended)
	if err != nil {
		fmt.Fprintln(stderr, "错误:", err)
		return 1
	}
	fmt.Fprintln(logOut, "\n画廊页面:", p)
	if cfg.Settings.AutoOpenBrowser && openBrowser {
		output.MaybeOpenBrowser(p)
		fmt.Fprintln(logOut, "已在浏览器中打开画廊")
	}
	fmt.Fprintln(logOut, "\n完成! 选中主题后点「用这个风格排版」即可复制到剪贴板。")
	return 0
}
