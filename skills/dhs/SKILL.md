---
name: dhs
description: >-
  使用 dhs CLI 查询 Docker Hub：按关键词搜索镜像仓库、浏览镜像的全部 Tag（版本）、
  按 CPU 架构（amd64/arm64/arm）过滤 Tag、生成可直接粘贴的 `docker pull` 命令、
  导出 Markdown 结果。

  触发场景：用户想找/挑选 Docker 镜像、问"某镜像有哪些 Tag/版本"、需要特定架构
  （如 ARM 服务器、Apple Silicon、树莓派）的镜像、要 docker pull 命令、
  说"在 Docker Hub 上搜 X"——即使没明说 Docker Hub 也应触发。

  不适用：镜像构建与 Dockerfile 编写、推送镜像、运行容器；
  把镜像同步到 Harbor / 国内仓库用 irs-sync（dsync），本 skill 只负责在 Docker Hub 上"找镜像"。
---

# dhs — Docker Hub 镜像查询

`dhs` 是一个查询 Docker Hub API 的小型 CLI，核心只有两个子命令：

- `dhs search <关键词>` — 按关键词搜索镜像*仓库*
- `dhs tags <镜像>` — 列出镜像的 *Tag*、按架构过滤、生成 `docker pull` 命令

绝大多数请求是两步跳：先 **search** 选定仓库，再 **tags** 选定版本并拿拉取命令。通过 Bash 运行 dhs，读取它输出的格式化表格。

## 前置检查与安装

**执行任何 dhs 命令前，先确认二进制可用**：

```bash
command -v dhs
```

没有时**必须先安装，不要直接执行 dhs**。按本机平台下载 Release 预编译二进制（linux/darwin/windows × amd64/arm64，替换文件名）：

```bash
# 例：linux amd64；darwin 用 dhs-darwin-amd64 / dhs-darwin-arm64，ARM 平台换 arm64
curl -fLo dhs https://github.com/lpx0312/dhs/releases/latest/download/dhs-linux-amd64
sudo install -m 0755 dhs /usr/local/bin/dhs

# GitHub 直连超时/失败时，在原 URL 前加代理前缀 https://gh.1102345.xyz/ 重试：
curl -fLo dhs https://gh.1102345.xyz/https://github.com/lpx0312/dhs/releases/latest/download/dhs-linux-amd64
```

Windows（PowerShell）：

```powershell
curl.exe -fLo dhs.exe https://github.com/lpx0312/dhs/releases/latest/download/dhs-windows-amd64.exe
# 然后把 dhs.exe 所在目录加入 PATH
```

安装后用 `dhs --help` 验证（注意：dhs **没有 `--version` 参数**，能打印用法即安装成功）。也可以从项目根源码编译：`go build -o dhs .` 后把二进制加入 PATH。

## 首次使用：一次性配置代理

本网络访问 Docker Hub 必须走代理。dhs 支持**持久化配置**——配置一次，之后所有命令自动带代理，无需每条都写 `--proxy`：

```bash
# 1. 看当前配置（文件值为 "-" 表示未设置过）
dhs config list

# 2. 未设置时写入默认代理（写入后对所有 dhs 命令永久生效）
dhs config set proxy http://192.168.0.225:7897

# 3. 确认生效值
dhs config get proxy    # 应输出 http://192.168.0.225:7897
```

配置文件位于平台标准目录（Windows `%APPDATA%\dhs\config.yaml`，Linux/macOS `~/.config/dhs/config.yaml`），可用 `--config` 指定其他路径。

生效优先级：`--proxy` 参数 > 配置文件 > `HTTPS_PROXY`/`HTTP_PROXY` 环境变量 > 直连。所以：
- **日常命令直接裸跑**：`dhs search nginx`、`dhs tags postgres`。
- 临时换代理时才加 `--proxy <URL>`，不改配置文件。
- `dhs config unset proxy` 可清除。

**旧版二进制兜底**：如果 `dhs config` 子命令不存在（旧版二进制，重新安装即可解决），改为每条命令显式带代理：

```bash
dhs --proxy "${DOCKERHUB_PROXY:-http://192.168.0.225:7897}" search nginx
```

（`DOCKERHUB_PROXY` 未设置时内联默认值兜底；最好顺手 `export DOCKERHUB_PROXY=http://192.168.0.225:7897` 写进 shell 配置。）

## 第 1 步 — 搜索仓库：`dhs search`

```bash
dhs search <关键词>            # 默认返回前 25 条
dhs search <关键词> -n 10      # 指定条数
```

输出列：`NAME`、`STARS`、`PULLS`、`OFFICIAL`（✓ 官方 / ✗ 社区）、`DESCRIPTION`。

解读结果：
- OFFICIAL 列为 `✓` 表示 Docker 官方维护的镜像（如 `nginx`、`redis`）——通常是最稳妥的默认选择。
- `user/repo` 形式（如 `bitnami/nginx`）是社区镜像；重要时说明维护方。
- 先展示表格，用户意图明确时补一句推荐。

## 第 2 步 — 查看 Tag：`dhs tags`

```bash
dhs tags nginx                       # 默认最新 10 个 Tag
dhs tags nginx --filter alpine       # 只看名字含 "alpine" 的 Tag
dhs tags nginx --all                 # 全部 Tag（可能有几百个）
dhs tags nginx --sort size           # 按 size | name | date 排序（默认 date）
dhs tags user/repo                   # 非官方镜像 → 用 user/repo 形式
```

输出列：`Tag`、`Status`、`Size(MB)`、`Architecture`、`LastUpdated`。

`nginx` 这种裸名字是官方 `library/nginx` 的简写；社区镜像要传完整 `user/repo`。注意搜索 API 不返回更新时间，所以 `search` 输出的是描述列——只有 `tags` 有 `LastUpdated`。

## 第 3 步 — 架构过滤 + 拉取命令

用户关心 CPU 架构时（ARM 服务器、Apple Silicon、树莓派很常见），`--arch` 配合 `--gen-pull` 使用：

```bash
dhs tags nginx --arch amd64,arm64 --gen-pull
```

- `--arch` 只保留包含指定架构的 Tag；逗号分隔、OR 语义（Tag 含*任一*列出架构即保留）。过滤看的是架构字段，所以 `--arch arm64` 能匹配 `arm64/v8`。
- `--gen-pull` 在表格后输出 `docker pull` 命令。**必须同时给 `--arch` 才生效**——不带 `--arch` 时 dhs 只打印提示并跳过，因为架构不固定时生成的命令有歧义。
- amd64 → 直接 `image:tag`（x86 主机的默认平台）。
- arm64 → `--platform=linux/arm64 image:tag`。

用户要保存或分享结果时导出 Markdown：

```bash
dhs tags nginx --arch amd64,arm64 --gen-pull -o nginx-tags.md
```

## 常见请求 → 命令映射

**例 1 —"帮我找个 redis 镜像"**
搜索后推荐官方镜像。
```bash
dhs search redis
```

**例 2 —"postgres 有哪些版本/Tag？"**
```bash
dhs tags postgres
```
要精简变体加 `--filter alpine`，看全部加 `--all`。

**例 3 —"给我 nginx 的 amd64 和 arm64 拉取命令"**
```bash
dhs tags nginx --arch amd64,arm64 --gen-pull
```

**例 4 —"找个轻量的 python 镜像，给我 ARM 服务器用的拉取命令"**
```bash
dhs search python                       # 选官方 `python`
dhs tags python --filter slim --arch arm64 --gen-pull
```

## 命令失败时排查

按顺序检查：
1. `dhs config list` 看生效代理是否为 `-`（未配置）——是则按上文 `config set proxy` 配置。
2. 已配置仍超时/被拒：多半是代理地址填错或代理挂了——和用户确认地址，不要自行换一个，并检查代理主机（`192.168.0.225:7897`）是否可达。
3. 需要走兼容镜像站时，可设 `DHS_API_BASE` 覆盖 API 端点（默认 `https://hub.docker.com`，尾斜杠自动去除；镜像需把分页 `next` 游标重写为自身 host，跨 host 游标会被拒绝）。
4. 输出要去色/管道场景：设 `NO_COLOR` 为非空值，或加 `--no-color`。

## 命令速查

全局参数（放在子命令前）：`--proxy URL` / `-P`、`--config PATH`、`--no-color`（等价环境变量 `NO_COLOR`，非空即禁色）；环境变量 `DHS_API_BASE` 可把 API 指向兼容镜像站。

| 子命令 | 说明 / 常用参数 |
|---|---|
| `search <关键词>` | `-n, --limit N`（默认 25） |
| `tags <镜像>` | `-n, --limit N`（10）、`--all` / `-a`、`--sort name\|size\|date`、`--order asc\|desc`、`--filter SUBSTR`、`--arch a,b`、`--gen-pull`、`-o FILE` |
| `config list` | 查看配置文件路径与各配置项、生效代理 |
| `config set <key> <value>` | 写入配置；key 为 `proxy` 或 `no-color` |
| `config get <key>` | 裸值输出，便于脚本 |
| `config unset <key>` | 清除配置项 |
