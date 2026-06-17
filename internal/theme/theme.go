package theme

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type Theme map[string]any

var GalleryThemes = []string{
	"core", "warm-card", "fresh-card", "ocean-card",
	"newspaper", "magazine", "ink", "coffee-house",
	"bytedance", "github", "sspai", "midnight",
	"terracotta", "mint-fresh", "sunset-amber", "lavender-dream",
	"sports", "bauhaus", "chinese", "wechat-native",
	"minimal-gold", "focus-blue", "elegant-green", "bold-blue",
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

func ResolveThemesDir(root string) string {
	return firstExistingDir(
		filepath.Join(root, "themes"),
		filepath.Join(root, "cmd", "format", "themes"),
		filepath.Join(root, "..", "themes"),
		filepath.Join(root, "..", "..", "themes"),
	)
}

func Load(root, themeName string) (Theme, error) {
	themesDir := ResolveThemesDir(root)
	if themesDir == "" {
		return nil, fmt.Errorf("未找到 themes 目录")
	}
	themePath := filepath.Join(themesDir, themeName+".json")
	if b, err := os.ReadFile(themePath); err == nil {
		var t Theme
		if err := json.Unmarshal(b, &t); err != nil {
			return nil, err
		}
		return t, nil
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
			if err := json.Unmarshal(layoutB, &layout); err != nil {
				return nil, err
			}
			if err := json.Unmarshal(paletteB, &palette); err != nil {
				return nil, err
			}
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
	return nil, fmt.Errorf("主题 '%s' 不存在。可用: %s", themeName, strings.Join(names, ", "))
}

func mergeLayoutPalette(layout Theme, palette map[string]string) (Theme, error) {
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
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		return nil, err
	}
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
	return out, nil
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
