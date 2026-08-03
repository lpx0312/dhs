# dhs (Docker Hub Search) CLI — 设计文档

- **日期**: 2026-08-03
- **状态**: 已确认，待实现
- **目标**: 用 Go 实现一个跨平台（Windows/Linux）的 CLI 工具 `dhs`，用于搜索 Docker Hub 上的镜像仓库和查询镜像 tag，并支持通过代理访问（Docker Hub 在部分地区需代理才能查询）。

## 1. 背景与参考

参考现有 PowerShell 脚本 `docker_hub_search.ps1`，其中实现了两个函数：
- `Get-DockerTags`：查镜像 tag，支持排序、过滤、分页、架构显示、导出文件。
- `Get-DockerArchImages`：查带 amd64/arm64 架构的 tag，并输出 `docker pull` 命令。

本工具在此基础上：
1. 跨平台（Go 编译为单二进制，Windows/Linux 通用）。
2. 新增**镜像仓库搜索**功能（ps1 没有）。
3. 把代理配置从写死的环境变量改为 flag + 配置文件 + 环境变量三级合并。
4. tag 子命令的 flag 改为 Unix 风格。
5. **合并 ps1 的两个函数**：原 `Get-DockerTags` 和 `Get-DockerArchImages` 统一为 `dhs tags` 一个命令。架构过滤（原 arch 命令的核心能力）变为 `--arch` flag，pull 命令生成变为 `--gen-pull` flag。两者查询逻辑一致（都是拉 tag 列表 + 分页），合并避免重复，且组合更灵活。

## 2. 依赖选型（方案 A）

| 用途 | 依赖 | 理由 |
|------|------|------|
| CLI 框架 | `github.com/spf13/cobra` | 子命令、持久化 flag、自动 help 最成熟 |
| 配置文件 | `gopkg.in/yaml.v3` | 可读性好，能写注释 |
| 彩色输出 | `github.com/fatih/color` | 自带 TTY 检测与 Windows VT 模式兼容 |
| HTTP | 标准库 `net/http` | 代理用 `http.ProxyURL`，无需第三方 |

## 3. 项目结构

```
dhs/
├── go.mod
├── main.go                     # 入口，仅 cobra.Execute()
├── cmd/                        # CLI 命令定义
│   ├── root.go                 # 根命令 + 持久化 --proxy/-P、--config、--no-color
│   ├── search.go               # dhs search <keyword>
│   └── tags.go                 # dhs tags <image>（含 --arch 架构过滤、--gen-pull）
├── internal/
│   ├── config/
│   │   └── config.go           # 配置加载（文件 + flag + 环境变量合并）
│   ├── dockerhub/
│   │   ├── client.go           # 带 proxy 的 http.Client、统一分页
│   │   ├── search.go           # 仓库搜索 API
│   │   └── tags.go             # tag 查询 API + 架构解析
│   └── ui/
│       ├── table.go            # 表格输出（对齐列宽）
│       └── color.go            # TTY 检测 + 彩色包装
└── README.md
```

## 4. 命令接口

### 4.1 全局（持久化）flag

```
dhs [--proxy|-P URL] [--config PATH] [--no-color] <command>
```

| flag | 说明 |
|------|------|
| `--proxy`, `-P` | 代理地址，如 `http://192.168.0.225:7892`，优先级最高 |
| `--config` | 配置文件路径，默认走平台标准路径 |
| `--no-color` | 关闭彩色输出 |

### 4.2 子命令

```bash
# 1. 搜索镜像仓库（新需求）
dhs search <keyword> [--limit|-n N]        # 默认 limit 25

# 2. 查镜像 tag（合并了 ps1 的两个函数）
dhs tags <image>                           # image 支持 nginx 或 user/nginx，省略 user 默认 library
  [--limit|-n N]            默认 10
  [--sort name|size|date]   默认 date
  [--order asc|desc]        默认 desc
  [--all|-a]                显示全部（覆盖 --limit）
  [--os]                    显示非 linux 架构时带 OS 前缀
  [--filter <str>]          tag 名模糊过滤
  [--arch <list>]           按架构过滤，逗号分隔，如 amd64,arm64 / arm64 / arm/v7
  [--gen-pull]              生成 docker pull 命令（常配合 --arch）
  [--output|-o FILE]        导出到 Markdown 文件
```

**ps1 函数到 dhs tags 的映射**：
- `Get-DockerTags` → `dhs tags nginx`
- `Get-DockerArchImages` → `dhs tags nginx --arch amd64,arm64 --gen-pull`

**`--arch` 精确匹配规则**：只匹配 `tag.images` 中 `architecture` 字段精确等于指定值的条目。如 `--arch arm64` 仅匹配 `architecture == "arm64"`（variant 为 `v8` 时仍匹配，因为 architecture 字段不变）。多值间是 OR 关系（命中任一即纳入）。

**`--gen-pull` 行为**：对结果中每个 tag，按其命中的架构生成 pull 命令。命中 amd64 生成 `<image>:<tag>`，命中 arm64 额外生成 `--platform=linux/arm64 <image>:<tag>`。library 镜像省略 `library/` 前缀。表格照常输出，pull 命令附在表格之后。

## 5. 配置合并

配置文件示例（YAML）：
```yaml
proxy: "http://192.168.0.225:7892"
no_color: false
```

**配置文件路径**（平台标准目录）：
- Windows: `%APPDATA%\dhs\config.yaml`（如 `C:\Users\<user>\AppData\Roaming\dhs\config.yaml`）
- Linux: `~/.config/dhs/config.yaml`

**合并优先级**（高 → 低）：
1. 命令行 `--proxy` flag（最高，临时覆盖）
2. 配置文件 `proxy` 字段（持久化默认值）
3. `HTTPS_PROXY` / `HTTP_PROXY` 环境变量（回退，与 ps1 行为一致）
4. 无代理直连（最低）

`--no-color` 同理覆盖配置文件 `no_color` 字段。

**加载逻辑**：
- `Load(cfgPath string) (*Config, error)`：`cfgPath` 非空用指定路径，否则用平台默认路径。
- 平台默认路径：`os.UserConfigDir()` + `/dhs/config.yaml`。
- 文件不存在不算错误，返回零值 Config（全靠 flag + 环境变量兜底）。

## 6. API 客户端

### 6.1 带 proxy 的 http.Client

```go
func NewClient(proxyURL string) (*http.Client, error) {
    // proxyURL 为空 → 默认 transport（仍读 HTTPS_PROXY 环境变量，因 Go 默认 ProxyFromEnvironment）
    // proxyURL 非空 → 解析并显式设置 http.ProxyURL，不读环境变量，避免歧义
}
```

**Go 标准库 proxy 行为**：
- `http.DefaultTransport.Proxy = ProxyFromEnvironment` 默认读 `HTTP_PROXY`/`HTTPS_PROXY`。即使用户不传 `--proxy`，只要设了环境变量也能走代理——与 ps1 行为天然一致。
- 用户传 `--proxy` 时用显式 `ProxyURL(u)` 覆盖。

### 6.2 API 端点与分页

| 功能 | 端点 | 分页 |
|------|------|------|
| 搜索仓库 | `GET /v2/search/repositories/?query=<kw>&page_size=<n>` | 单页请求，`page_size=limit`，不翻页 |
| 查 tag | `GET /v2/repositories/{user}/{image}/tags/?page_size=100&page=<p>` | 循环到 `next` 为空或达到 limit |

**两种分页语义不同**：
- **search**：只取一页（`page_size` 等于 `--limit`），搜索结果按相关度排序，取 top N 即可，不翻页。
- **tags/arch**：镜像 tag 可能成百上千，需循环翻页。模式如下：
```go
for {
    resp := GET(url)
    append(results, resp.Results...)
    if resp.Next == "" || (limit > 0 && len(results) >= limit) { break }
    url = resp.Next          // 直接用 API 返回的 next URL，避免自己拼 page 号
    sleep(500ms)             // 礼貌限速，对齐 ps1 的 Start-Sleep 500ms
}
```

### 6.3 架构解析逻辑

复刻 ps1 第 60-94 行核心规则，用 Go 的 map 去重：
- 遍历 `tag.images`，跳过 `architecture == "unknown"`。
- 有 `variant` 则拼 `arch/variant`（如 `arm/v7`）。
- 默认只收 `os == "linux"` 的架构（与 ps1 不带 `-ShowOS` 一致）。
- `--os` flag 开启时：非 linux 架构加前缀（如 `windows/amd64`）。
- 用 `map[string]struct{}` 去重后排序输出。

**`--arch` 过滤**：解析 flag 得到目标架构集合，对每个 tag 判断其 linux 架构（`architecture` 字段，不含 variant）是否命中目标集合任一值。命中则保留，未命中则过滤掉。与表格中显示的架构列独立——显示列可能含 `arm64/v8`，但过滤判断只看 `arm64`。

**`--gen-pull` 命令生成**：对过滤后保留的每个 tag，检查其命中的目标架构：命中 `amd64` 则生成 `<image>:<tag>`，命中 `arm64` 额外生成 `--platform=linux/arm64 <image>:<tag>`。仅当同时指定了 `--arch` 时 `--gen-pull` 才生效（无 `--arch` 时 tag 内架构混杂，生成 pull 命令无意义，此时忽略 `--gen-pull` 并给出提示）。

## 7. 数据流

以 `dhs tags nginx --arch amd64,arm64 --gen-pull` 为例：
```
root.go 持久化 flag 解析
  → config.Load 合并代理优先级
  → dockerhub.NewClient(proxy) 构造带代理的 http.Client
  → tags.go RunE: 解析镜像名（nginx → library/nginx）
  → dockerhub.FetchTags(client, "library", "nginx", limit/all)
  → 本地处理流水线：
      ① --filter  按名模糊过滤（可选）
      ② --arch    按架构过滤（可选）
      ③ --sort    排序（name/size/date）
      ④ --limit/--all  截断
  → ui.PrintTable(tags)        # 表格照常输出
  → 若 --gen-pull: ui.PrintPullCommands(tags, image)  # 表格后附 pull 命令
  → 若 --output:  写 Markdown 文件
```

## 8. 错误处理

| 场景 | 处理 | 退出码 |
|------|------|--------|
| 代理 URL 格式错误 | 致命，提示 `invalid proxy URL: ...` | 1 |
| 配置文件 YAML 格式错误 | 致命，提示文件路径 + 解析错误位置 | 1 |
| 配置文件不存在 | **正常**，静默用零值 | 0（继续执行） |
| HTTP 401/403 | 提示访问被拒绝、可能已限流 | 1 |
| HTTP 404（镜像不存在） | 提示镜像不存在 | 1 |
| HTTP 超时/连接失败 | 提示连接失败，错误信息带当前代理地址 | 1 |
| 镜像名为空 | cobra 参数校验直接报错 | 1 |

网络错误时若设了代理，错误信息带上当前代理地址，方便排查。

## 9. 跨平台

- 路径处理：全用 `filepath.Join`，配置路径用 `os.UserConfigDir()`，不写死分隔符。
- 彩色输出：`fatih/color` 内部已处理 Windows Terminal/ANSI 兼容，`--no-color` 和非 TTY 自动关闭。
- 代理：`http.ProxyURL` 跨平台一致。
- 无 `runtime.GOOS` 分支，所有平台差异委托给标准库和依赖库。

## 10. 测试策略

- `internal/dockerhub/`：用 `httptest.Server` 起 mock 服务器，返回预构造 JSON，测试分页循环、架构解析、next URL 跟随、limit 截断。不打真实 Docker Hub，测试可离线、可重复。
- `internal/config/`：测试优先级合并（flag > 文件 > 环境变量）、文件不存在的处理、YAML 解析。
- `internal/ui/`：测试表格对齐（给定固定输入断言输出字符串）。
- `cmd/`：轻量，靠子命令集成。
- 目标覆盖率：dockerhub、config 80%+，ui 60%+。

## 11. 输出样例

`dhs search nginx`：
```
NAME                  STARS    STARS       OFFICIAL   DESCRIPTION
nginx                 21352    13220858430 ✓          Official build of Nginx.
nginx/nginx-ingress   122      1086428245  ✗          NGINX and NGINX Plus Ingress Controlle…
...
找到 25 个结果（共 290456）
```

> **注意**：Docker Hub 的 search API (`/v2/search/repositories/`) 返回的仓库名字段是 `repo_name`（不是 `name`），且**不返回** `last_updated`。因此 search 输出用 `short_description` 作为最后一列，而非更新时间。（此修正基于 2026-08-03 实测真实 API 响应。）

`dhs tags nginx`（默认 date desc, limit 10）：
```
镜像: library/nginx

Tag           Status    Size(MB)   Architecture          LastUpdated
latest        active    192.5      amd64, arm64/v8      2026-07-15
1.27-alpine   active    45.2       amd64, arm64/v8      2026-07-10
...

找到 856 个标签，显示前 10 个
提示: 使用 --all 参数查看全部 856 个标签
```

`dhs tags nginx --arch amd64,arm64 --gen-pull`（架构过滤 + 生成 pull 命令）：
```
镜像: library/nginx
找到 850 个包含 amd64/arm64 架构的标签，显示前 10 个

Tag           Status    Size(MB)   Architecture          LastUpdated
latest        active    192.5      amd64, arm64/v8      2026-07-15
...

=== Docker 镜像拉取命令 ===
nginx:latest
--platform=linux/arm64 nginx:latest
nginx:1.27-alpine
--platform=linux/arm64 nginx:1.27-alpine

共生成 20 条命令
```

**文件导出格式**：`--output` 写 Markdown 表格（比 ps1 的 TSV 更通用，GitHub/编辑器直接渲染），命令列表部分同 ps1 纯文本。library 镜像在 pull 命令里省略 `library/` 前缀（与 ps1 第 361 行一致）。
