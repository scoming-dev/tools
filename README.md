# tools

`github.com/scoming-dev/tools` 是一组 Go 业务工具包，覆盖文档转换与生成、对象存储、认证授权、缓存、编解码、校验、图片、短信等常见需求。

- 环境：Go 1.26.1
- 安装：`go get github.com/scoming-dev/tools`
- 测试：`go test ./...`，格式化使用 `gofmt`
- DOCX、OSS、验证码、短信等包需要自备对应的第三方服务或密钥

## 包一览

| 包 | 说明 |
| --- | --- |
| `docx` | Markdown 转 DOCX：标题、目录、列表、表格、图片、SVG、原生 Office/WPS 公式、可扩展插件。 |
| `markdown` | DOCX、XLSX、PPTX、PDF、EPUB、HTML、邮件、压缩包、图片、文本转 Markdown。 |
| `oss` | 统一对象存储接口，兼容 MinIO、阿里云 OSS、华为 OBS 旧调用方式。 |
| `oss/miniox` `oss/aliyunx` `oss/huaweix` | 三家云存储的独立适配，互不引入其他厂商 SDK。 |
| `captcha` | 点击、滑块、旋转验证码生成与校验，存储由使用方注入。 |
| `onlyoffice` | OnlyOffice 文档类型识别、JWT、文档配置构建。 |
| `authx` | 密码哈希、Bearer 解析、JWT、API Key、HTTP 鉴权中间件、Refresh Token、缓存 Session。 |
| `oauthx` | OAuth2 授权 URL、PKCE、state 防重放、授权码/刷新 token、常见 provider 预设。 |
| `permissionx` | `resource:action` 权限码生成、解析、通配匹配、批量判断。 |
| `casbinx` | Casbin RBAC/租户域模型构建与权限操作封装，不绑定数据库；`casbinx/gormx` 提供 GORM 持久化。 |
| `cache` | 通用缓存接口与并发安全的内存实现，支持 TTL、SetNX、Remember。 |
| `filex` | 文件判断、大小格式化、MIME、hash、base64、HTTP 下载。 |
| `jsonx` | JSON 编解码、压缩、美化、校验、深拷贝、map 转换、点路径读写。 |
| `stringx` | 字符串截取、补齐、脱敏、命名风格转换、去重、安全随机串。 |
| `networkx` | 客户端 IP、IPv4/IPv6、公网/内网判断、CIDR、HostPort、可用端口、TCP 探测。 |
| `validator` | 手机号、邮箱、URL、IP、身份证、统一社会信用代码、银行卡、金额、密码强度校验。 |
| `httpx` | HTTP 客户端：JSON、query、默认 header、重试、状态错误、文件上传下载。 |
| `imagex` | 图片尺寸与 MIME、base64、JPEG 压缩、等比缩放、多格式保存。 |
| `crypto` | MD5、SHA256、HMAC-SHA256、安全随机串、AES-GCM/CBC、RSA 签名验签。 |
| `excel` | XLSX 导出与读取，支持 map/slice、表头样式、冻结表头、筛选。 |
| `money` | 基于 decimal 的金额运算、格式化、元分转换、人民币大写。 |
| `sms` | 聚合短信接口，内置阿里云、腾讯云、云片、Submail、聚合数据、螺丝帽、创蓝等适配。 |
| `uniqueid` | 业务单号与 Sonyflake ID 生成。 |

## markdown：文档转 Markdown

```go
converter := markdown.New(
	markdown.WithAssetsDirectory("report_files", "report_files"),
)
result, err := converter.Convert(context.Background(), "report.docx")
if err != nil {
	log.Fatal(err)
}
fmt.Println(result.Markdown)
```

- `New()` 启用全部内置转换器；只需要部分格式时用 `NewCore` 按组开启（`WithTextConverters`、`WithOfficeConverters`、`WithArchiveConverters`、`WithImageConverter`、`WithPDFConverter`），或创建后调用对应的 `Register...` 方法，重复注册不会重复添加。
- 表格默认输出 HTML 以保留合并单元格；需要纯 Markdown 表格时用 `markdown.WithTableFormat(markdown.TableFormatMarkdown)`，含合并单元格的表格仍用 HTML。
- 图片默认由 `WithAssetsDirectory` 落盘并在 Markdown 中保留相对链接，也可用 `WithImageHandler` 上传对象存储后返回 URL；内容相同的图片只保存一份，同名不同内容自动加后缀。
- 输入默认不限大小（归档输入固定为单成员 32MB、512 个成员、4 层嵌套）；防护不可信上传时用 `WithMaxInputSize` 显式限制。

### PDF

PDF 全部本地解析，不依赖远程服务，也不做 OCR。引擎是 MuPDF（经 [go-fitz](https://github.com/gen2brain/go-fitz) 调用，CGO 静态链接），从文字层重建段落、标题（`#`–`###`）与表格网格，图片压缩后外置保存。

没有文字层的页面按它剩下的内容处理：扫描件只保留图片引用；没有任何可用内容的页面整页渲染。图纸这类"文字已转曲、引擎只报出图签印章"的页面同样整页渲染——没有文字、且仅有的图片加起来不到整页 20%（印章、图签、水印这一档）的页面按"只剩装饰"处理，否则每页只会导出一枚重复的印章而丢掉整张图。要文字仍需另行 OCR（整页渲染出的图片就是现成的 OCR 输入）。

```go
markdown.WithPDFOptions(markdown.PDFOptions{
	RenderDPI:        200,                         // 整页渲染分辨率，也是内嵌图片的有效分辨率上限
	RenderFormat:     markdown.PDFImageFormatJPEG, // 默认 JPEG，设为 PNG 则无损
	JPEGQuality:      85,
	MaxImagesPerPage: 32, // 超过则整页渲染，避免图纸页炸出上千张图
	FirstPage:        0,  // 0 表示第一页
	LastPage:         0,  // 0 表示最后一页
})
```

另有 `MaxImages`、`MaxRenderedPages`，以及 `DisablePageRenderFallback`、`DisableTableReconstruction`、`DisableHeadingDetection`、`DisableParagraphReflow` 等保守输出开关。页面默认并行转换（按 CPU 数，上限 8；`PageConcurrency` / `--pdf-workers` 可调，设为 1 即串行），并行输出与串行逐字节一致；设置了图片或渲染页预算时会自动退回串行以保证结果可复现。

> **许可**：MuPDF 采用 AGPL-3.0（也可向 Artifex 购买商业授权）。对外分发或作为网络服务提供前请确认合规；跨平台构建需要目标平台的 C 工具链，不能纯 Go 交叉编译。

### 命令行

```bash
go run ./cmd/markdown -i report.docx -o report.md
go run ./cmd/markdown -i report.pdf -o report.md --assets-dir images --pdf-dpi 300 --pdf-image-format jpeg
make build-markdown-cli-all   # 输出 bin/markdown-<os>-<arch>
```

常用参数：`--table-format`、`--assets-dir`、`--assets-prefix`、`--max-input-size`，PDF 相关有 `--pdf-dpi`、`--pdf-image-format`、`--pdf-jpeg-quality`、`--pdf-first-page`、`--pdf-last-page`、`--pdf-max-images-per-page`、`--pdf-max-images`、`--pdf-max-rendered-pages`、`--pdf-workers` 以及 `--pdf-no-tables`、`--pdf-no-headings`、`--pdf-no-reflow`、`--pdf-no-render-fallback`。不传 `-o` 时输出到标准输出，附件目录建在当前目录；`--assets-dir` 为相对路径且指定了 `-o` 时，相对于输出文件所在目录解析。

### 代码结构

`markdown` 包只保留公开 API（类型别名保证兼容），各格式实现按格式拆到 `internal/`：

| 位置 | 职责 |
| --- | --- |
| `internal/core` | 转换模型与公共层：`Result`/`Image`/`ImageHandler`/`Converter`、`Settings`、表格与代码块渲染、图片落盘、`PDFOptions`。 |
| `internal/ooxml` | DOCX 与 PPTX 共用的合并单元格表格网格。 |
| `internal/docx` `internal/pptx` `internal/xlsx` | 三种 Office 格式。 |
| `internal/pdf` | 本地 PDF；`engine.go` 是唯一接触 MuPDF 的地方，`pdf_layout.go` 做引擎无关的版面重建。 |
| `internal/text` `internal/email` `internal/archive` `internal/imagefile` | 文本与结构化文本、EML 邮件、ZIP/EPUB、独立图片。 |

新增格式时，在对应 `internal/` 子包实现 `NewConverter(settings *core.Settings) core.Converter`，再挂到 `converter.go` 的注册表。

## docx：Markdown 转 DOCX

```go
markdown := []byte("# 第一章\n\n正文内容。\n\n行内公式 $a^2+b^2=c^2$\n")

parseOptions := parse.NewOptions()
parseOptions.HTMLTag2TextMark = true
tree := parse.Parse("", markdown, parseOptions)

renderOptions := render.NewOptions()
renderOptions.RenderListStyle = true

renderer := docx.NewDocxRenderer(tree, renderOptions, docx.HeadingStyleDefault)
renderer.Render()
if err := renderer.Save("tmp/example.docx"); err != nil {
	log.Fatal(err)
}
```

- 列表使用 DOCX 原生 numbering，报告正文层级按 `（一）`、`1.`、`（1）`、`①`、`A.`、`a.`、`1）` 循环。
- 公式输出 Office Math Markup Language，SVG 按 SVG 媒体嵌入，均不使用图片或外部转换命令。
- 测试产物写到 `docx/tmp/`，该目录保持 git 忽略。

### 命令行

```bash
make build-docx-cli-all   # 输出 bin/docx-<os>-<arch>[.exe]
bin/docx-darwin-arm64 -input report.md -output tmp/report.docx -cover none -heading-style default
bin/docx-darwin-arm64 -input report.md -output tmp/report.docx -table-tags excel
```

参数：`-heading-style`（`default`/`center-page-break`/`left-page-break`/`0`/`1`/`2`）、`-cover`（`none`/`report`/`special-debt`/`embodiment`）、`-table-tags`（把 `<s-tag type="excel" ...>` 渲染为 Excel 表格）、`-business-presets`、`-format`；不带参数时打印帮助。二进制以 `-trimpath -ldflags="-s -w"` 构建。

## 其他示例

OSS：

```go
client, err := oss.NewOSSClient(oss.OssConfig{
	Type: "minio",
	Minio: &oss.MinioConfig{
		Endpoint: "localhost:9000", AccessKeyID: "minioadmin",
		SecretAccessKey: "minioadmin", BucketName: "example", UseSSL: false,
	},
})
url, objectName, err := client.UploadFileWithPrefix(context.Background(), "tmp/example.docx", "doc")
```

更多配置见 [oss/README.md](oss/README.md)。

验证码（不绑定 Redis，实现 `captcha.Store` 即可，`captcha.WithTTL(...)` 可调过期时间）：

```go
capt := captcha.NewCaptcha(ctx, captcha.NewMemoryStore())
err, payload := capt.SlideCapt()
```

OnlyOffice：

```go
cfg, err := onlyoffice.BuildConfig(&onlyoffice.Config{
	DocumentId: "doc-1", DocumentName: "report.docx", DocumentKey: "report-doc-1",
	CallbackUrl: "https://example.com/app/base/onlyoffice/callback", Secret: "your-secret",
})
```

## 开发约定

- 不提交 `tmp/`、`docx/tmp/`、测试输出文档、服务密钥和本地配置文件。
- 涉及第三方服务的测试使用临时凭据或 mock，避免把真实密钥写入仓库。
