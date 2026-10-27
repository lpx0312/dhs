# Changelog

本项目的所有显著变更都记录在此文件中。

格式基于 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/)，
版本管理遵循 [SemVer](https://semver.org/lang/zh-CN/)。

## [Unreleased] → v0.3.0

本版本来自一次全仓代码审查驱动的质量收敛：10 个并行审查任务 → 约 100 个新增测试钉住缺陷 → 两轮多角色（安全/Go 实现/测试质量/CLI 体验）交叉 review → 修复 + 模块文档。

### 修复（Fixed）

**颜色与输出**

- `NO_COLOR=1` 被忽略、管道/重定向输出仍含 ANSI 转义码：`SetEnabled(true)` 无条件覆写 fatih/color 的 init 期检测。现在 `SetEnabled` 只禁不开，颜色优先级为 `--no-color` > 配置 `no-color` > `NO_COLOR` env（no-color.org 约定：非空即生效，空值忽略）> 自动 TTY 检测（`internal/ui/color.go`、`internal/config/config.go`、`cmd/root.go`）。
- 表格列宽把 ANSI 转义字节计入：彩色与无色模式下同一列的可见偏移不同（如 OFFICIAL 列的 ✓/✗）。宽度计算现在先剥离 ANSI 再计量，输出仍保留颜色（`internal/ui/table.go`）。
- `FormatPullCommands` 对重复 tag 名的输出顺序不确定（`sort.Slice` 非稳定）：改用 `sort.SliceStable`。

**网络层（internal/dockerhub）**

- `DHS_API_BASE` 带尾斜杠时拼出 `//v2/...` 双斜杠 URL：`BaseURL()` 现在自动去除尾斜杠。
- 分页游标 `next` 处理全面加固（`resolveNext`）：
  - 相对路径游标（`/v2/...?page=2`）此前报 `unsupported protocol scheme ""`，现在相对 base 正确解析；
  - 指向配置 base 之外 host 的游标此前被静默跟随（镜像站场景第 2 页流量穿透回真实 Docker Hub），现在拒绝并报错；
  - 协议相对 URL（`//evil.example/...`）可绕过 host 校验（第一轮 review 实测发现），现在归一化后统一校验 scheme+host+userinfo；
  - 异 scheme（`http://` 对 `https` base，降级明文）与注入 userinfo（`https://evil@hub...`）均拒绝；
  - 新增分页环保护（重复游标即报错）与 `maxPages=1000` 硬上限，防 `--all` 死循环。
- `user`/`image` 路径段未转义：镜像名含 `#`、`?`、空格时构造出错误请求（`a#b` 的 `#` 后被截成 fragment 且 `page_size` 丢失）。现在使用 `url.PathEscape`。
- HTTP 非 2xx 错误消息现在带上出错的完整 URL（分页场景可区分第几页失败），且经 `redactURL` 脱敏——URL 中的 userinfo 密码呈 `user:xxxxx@host` 形式，不再明文外泄。
- `cmd/root.go` 的 `errWithProxy` 此前把可能内嵌凭据的代理 URL 原样打印到 stderr，现在经 `url.Redacted()` 脱敏。

**命令层（cmd）**

- `SilenceUsage: true`：此前任何运行期错误（网络失败、HTTP 429、坏 YAML）都会在 `Error:` 后倾倒整段 usage 文本，淹没真实错误；现在仅保留 Error 行（flag 语法错误仍有自解释的错误消息）。

**测试基建**

- `e2e` 黑盒测试的 `TestMain` 中 `defer os.RemoveAll` 在 `os.Exit` 下永不执行，每次运行泄漏一个 TEMP 目录（含约 10MB 二进制）：改为显式清理。
- e2e 子进程增加 30s 超时保护（`exec.CommandContext`），CLI 卡死不再挂住整个测试；`NO_COLOR=` 空值改为实际生效的 `NO_COLOR=1`。

### 变更（Changed）

- 错误消息格式：`docker hub tags returned HTTP 502` → `docker hub tags returned HTTP 502 for https://hub.docker.com/v2/...`（稳定前缀不变，行尾追加 URL；全串精确匹配的脚本需更新，`grep "HTTP 502"` 类前缀匹配不受影响）。
- 管道/重定向输出不再包含 ANSI 颜色码（此前是 bug 行为，详见迁移指南）。

### 新增（Added）

- `NO_COLOR` 环境变量支持与优先级链（见上）。
- `DHS_API_BASE` 环境变量正式文档化：指向兼容镜像站，默认 `https://hub.docker.com`；分页游标必须留在同一 host（镜像需把 API 返回的 `next` 重写为指向自身）。
- `.gitattributes`：Go/模块/工作流文件强制 LF 检出，消除 Windows `autocrlf` 配置与 ubuntu CI `gofmt` 的 CRLF 误报冲突。
- `docs/modules/` 五份模块文档（cmd、internal-config、internal-dockerhub、internal-ui、e2e-and-ci），含职责、数据流、测试指南与带优先级的重构方案。

### 测试（Tests）

- 测试总数从 143 增至 240（含表驱动子测试），全部离线（httptest）。
- 总覆盖率 97.6%（cmd 96.7% / config 97.5% / dockerhub 96.2% / ui 100%）。
- `go test -shuffle` 乱序运行通过（无隐藏顺序依赖）。
- `internal/ui/ui_test.go` 并入 `table_test.go`（消除重复），新增钉现状/钉修复约定注释。

### 构建（Build）

- `go mod tidy`：cobra/color/yaml.v3 从错误的 `// indirect` 标记中修正为直接依赖。
- Gitea release 工作流补 `CGO_ENABLED=0`：与 GitHub 版对齐，产物静态链接，可在 Alpine/musl、scratch 容器与老 glibc 发行版上运行（此前 Gitea 产物动态链接 glibc）。

## [v0.2.0] 及更早

见 git 历史（tag `v*`）。
