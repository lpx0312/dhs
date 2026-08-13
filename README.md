# dhs - Docker Hub Search CLI

一个用 Go 编写的跨平台（Windows/Linux/macOS）命令行工具，用于搜索 Docker Hub 上的镜像仓库和查询镜像 tag，支持通过代理访问。

## 功能

- **`search`** — 按关键字搜索 Docker Hub 镜像仓库（名称、star 数、拉取数、官方标记、描述）
- **`tags`** — 查询镜像的 tag 列表，支持排序、过滤、架构筛选、生成 `docker pull` 命令、导出 Markdown 文件

## 作为 Claude Code 插件市场安装

本仓库同时是一个 **Claude Code 插件市场**（组织方式参考 [kepano/obsidian-skills](https://github.com/kepano/obsidian-skills)），将 `dhs` 技能以 Skill 形式提供给 Claude 使用。

### Marketplace（推荐）

在 Claude Code 中执行：

```
/plugin marketplace add lpx0312/dhs
/plugin install dhs@dhs
```

### npx skills

```bash
npx skills add git@github.com:lpx0312/dhs.git
```

### 手动安装

将 `skills/dhs/` 目录复制到 Claude 的 skills 目录：

- **Claude Code**：`~/.claude/skills/dhs/`
- **Claude Desktop（项目级）**：`.claude/skills/dhs/`

### 市场结构

```
dhs/
├── .claude-plugin/
│   ├── marketplace.json      # 市场清单（Claude Code 识别入口）
│   └── plugin.json           # 插件清单（名称/描述/作者）
└── skills/
    └── dhs/
        └── SKILL.md          # dhs 技能定义
```

> 安装后，在对话中直接说"帮我搜索一个 mysql 镜像"即可自动调用 dhs 技能。

## 安装

```bash
go build -o dhs .
# Windows: 生成 dhs.exe
# 跨平台纯静态编译（推荐，无 C 依赖）:
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o dhs .
```

## 快速开始

```bash
# 搜索镜像（需要代理时用 --proxy）
dhs search nginx
dhs --proxy http://127.0.0.1:7890 search redis

# 查询 tag
dhs tags nginx                      # 默认按时间倒序，前 10 个
dhs tags nginx -n 20                # 前 20 个
dhs tags nginx --sort size          # 按大小排序
dhs tags nginx --filter alpine      # 过滤含 alpine 的 tag
dhs tags nginx --all                # 全部 tag

# 架构筛选 + 生成 pull 命令（对应原 ps1 的 Get-DockerArchImages）
dhs tags nginx --arch amd64,arm64 --gen-pull

# 导出 Markdown
dhs tags nginx --arch amd64,arm64 --gen-pull -o nginx-tags.md
```

## 代理配置

代理按以下优先级合并（高 → 低）：

1. `--proxy` / `-P` 命令行 flag（最高）
2. 配置文件 `proxy` 字段
3. `HTTPS_PROXY` / `HTTP_PROXY` 环境变量
4. 直连（不使用代理）

### 配置文件

位置（平台标准目录）：
- **Windows**: `%APPDATA%\dhs\config.yaml`
- **Linux**: `~/.config/dhs/config.yaml`

示例：

```yaml
proxy: "http://192.168.0.225:7897"
no_color: false
```

## 命令参考

### 全局 flag

| flag | 说明 |
|------|------|
| `-P, --proxy` | 代理地址，如 `http://host:port` |
| `--config` | 配置文件路径（默认走平台标准目录） |
| `--no-color` | 关闭彩色输出 |

### `dhs search <keyword>`

| flag | 说明 | 默认 |
|------|------|------|
| `-n, --limit` | 最大结果数 | 25 |

### `dhs tags <image>`

| flag | 说明 | 默认 |
|------|------|------|
| `-n, --limit` | 最大显示 tag 数 | 10 |
| `--sort` | 排序字段：`name` / `size` / `date` | `date` |
| `--order` | 排序方向：`asc` / `desc` | `desc` |
| `-a, --all` | 显示全部 tag（覆盖 --limit） | false |
| `--os` | 显示非 linux 架构时带 OS 前缀 | false |
| `--filter` | tag 名模糊过滤 | — |
| `--arch` | 按架构过滤，逗号分隔（如 `amd64,arm64`） | — |
| `--gen-pull` | 生成 docker pull 命令（需配合 `--arch`） | false |
| `-o, --output` | 导出 Markdown 文件 | — |

> `image` 支持 `nginx`（默认 `library`）或 `user/repo` 格式。

## 开发

```bash
go test ./...                      # 运行全部测试（离线，用 httptest）
go vet ./...                       # 静态检查
CGO_ENABLED=0 GOOS=linux go build  # 交叉编译
```

测试全部基于 `httptest`，不依赖真实 Docker Hub 网络，可离线重复运行。

## 代码仓库与推送

代码同时镜像到两个 Git 托管平台，已配置为**一次 `git push` 同时推送到两端**：

| remote | 平台 | 地址 |
|--------|------|------|
| `origin` | Gitea（主仓库） | `ssh://git@192.168.0.180:9020/study/dhs.git` |
| `github` | GitHub（镜像） | `https://github.com/lpx0312/dhs.git` |

`origin` 的 push 地址同时指向 Gitea 与 GitHub，因此普通推送即可同步两端：

```bash
git push                  # 自动推送到 Gitea + GitHub
git push origin v0.2.0    # 推送 tag 同样同步两端
```

首次配置命令（仅供参考，仓库已完成配置）：

```bash
git remote add github https://github.com/lpx0312/dhs.git
git remote set-url --add --push origin ssh://git@192.168.0.180:9020/study/dhs.git
git remote set-url --add --push origin https://github.com/lpx0312/dhs.git
```

> `git fetch` / `git pull` 默认从 Gitea（`origin` 的 fetch 地址）拉取；需要从 GitHub 拉取时用 `git fetch github`。

## 发布

支持三种发布方式：
- **手动发布**：本地编译 + tea CLI 上传到 Gitea → [发布指南](docs/release-guide.md)
- **Gitea Actions 自动流水线**：推送 `v*` tag 触发 `.gitea/workflows/release.yml`（多平台） → [发布指南 - 自动流水线](docs/release-guide.md#二自动流水线gitea-actions)
- **GitHub Actions 自动流水线**：推送 `v*` tag 触发 `.github/workflows/release.yml`，交叉编译 `linux/amd64` + `linux/arm64` 并发布 GitHub Release

由于 tag 推送也走双推送，打 tag 后 Gitea 与 GitHub 两条流水线会**同时**触发：

```bash
git tag v0.2.0 -m "v0.2.0"
git push origin v0.2.0          # 同步到 Gitea + GitHub，两端各自触发 Release
```

Release 下载页：
- Gitea：`http://192.168.0.180:9000/study/dhs/releases`
- GitHub：`https://github.com/lpx0312/dhs/releases`

## 项目结构

```
dhs/
├── main.go                  # 入口
├── cmd/                     # cobra 命令
│   ├── root.go              # 根命令 + 持久化 flag + 共享 helper
│   ├── search.go            # dhs search
│   └── tags.go              # dhs tags（含 --arch/--gen-pull）
├── .gitea/workflows/        # Gitea Actions 流水线
│   └── release.yml          # Gitea 自动构建 + 发布（多平台）
├── .github/workflows/       # GitHub Actions 流水线
│   └── release.yml          # GitHub 自动构建 + 发布（linux/amd64+arm64）
├── .claude-plugin/          # Claude Code 插件市场清单
│   ├── marketplace.json     # 市场清单
│   └── plugin.json          # 插件清单
├── docs/
│   └── release-guide.md     # 发布指南（手动 + 自动）
├── skills/
│   └── dhs/                 # dhs 使用 skill（搜索 Docker Hub 镜像）
└── internal/
    ├── config/              # 配置加载与代理合并
    ├── dockerhub/           # API 客户端（分页、架构解析）
    └── ui/                  # 彩色、表格、pull 命令
```
