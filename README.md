# format

`format` 用于把 Markdown 排版成适合微信公众号编辑器粘贴的 HTML。默认读取剪贴板内容，排版后再写回剪贴板，适合日常快速复制粘贴。

## 使用方式

```powershell
go run . [参数]
```

常用参数：

- `<file>` 或 `-i <file>`：从 Markdown 文件读取内容。
- `--std`：没有 `-i` 时从标准输入读取；不打开浏览器时输出到标准输出。
- `-t <theme>`：指定主题，例如 `bauhaus`、`wechat-native`。
- `-w`：生成 `preview.html`，并按配置决定是否自动打开浏览器。
- `-o <dir>`：指定输出目录。
- `-format <wechat|html|plain>`：指定输出格式，默认 `wechat`。
- `-gallery`：生成主题画廊。
- `-recommend <ids>`：画廊模式下推荐主题 ID，多个用逗号分隔。
- `-vault-root <dir>`：指定 Obsidian Vault 根目录，用于解析 wikilink 和图片。

## 输入规则

传入文件路径时，输入来自指定 Markdown 文件，`-i` 可以省略：

```powershell
go run . article.md -t bauhaus
go run . -i article.md -t bauhaus
```

没有文件路径和 `-i` 时，默认从剪贴板读取 Markdown：

```powershell
go run . -t bauhaus
```

没有 `-i` 且带 `--std` 时，从标准输入读取 Markdown：

```powershell
Get-Content article.md -Raw | go run . --std -t bauhaus
```

## 输出规则

默认不打开浏览器，此时 `wechat` 内容会以富文本 HTML 写入剪贴板，不生成 `preview.html`：

```powershell
go run . -i article.md
```

带 `--std` 且不打开浏览器时，`wechat` 内容以 HTML 字符串输出到标准输出，状态信息输出到 stderr：

```powershell
Get-Content article.md -Raw | go run . --std > article.wechat.html
```

带 `-w` 时会生成预览页面：

```powershell
go run . -i article.md -w
```

预览文件路径通常是：

```text
<output_dir>/<文章文件名>/preview.html
```

是否自动打开浏览器由 `config.json` 中的 `settings.auto_open_browser` 和命令行 `-w` 共同决定。

## 示例

从剪贴板读取 Markdown，排版后复制微信富文本内容到剪贴板：

```powershell
go run .
```

从文件读取并生成浏览器预览：

```powershell
go run . -i article.md -t bauhaus -w
```

作为管道工具使用：

```powershell
Get-Content article.md -Raw | go run . --std -t wechat-native > article.wechat.html
```

生成主题画廊：

```powershell
go run . -i article.md -gallery -recommend bauhaus,wechat-native
```
