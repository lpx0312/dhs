# dhs v0.3.0 迁移指南

适用版本：从 v0.2.0 升级到 v0.3.0。本版本没有移除任何命令或 flag，绝大多数用户无感升级；
以下条目按"你是否有对应用法"自取。

## 1. 脚本解析 dhs 的错误输出

**变更**：HTTP 错误消息末尾追加了出错的完整 URL。

```text
# v0.2.0
docker hub tags returned HTTP 502

# v0.3.0
docker hub tags returned HTTP 502 for https://hub.docker.com/v2/repositories/library/nginx/tags/?page_size=100
```

- 前缀/包含匹配（`grep "HTTP 502"`、`grep "not found"`）**不受影响**。
- 行尾锚定（`grep 'HTTP 502$'`）或全串精确匹配的脚本需要去掉锚定。
- 附带效果：URL 中若含凭据（如 `DHS_API_BASE=https://user:pass@mirror`），错误输出中呈 `user:xxxxx@mirror` 脱敏形态，不会泄漏明文密码。

## 2. 管道/重定向输出中的 ANSI 颜色码

**变更**：`dhs search nginx > out.txt` 或经管道（`|`、CI 日志）时，输出**不再包含** ANSI 转义码（v0.2.0 因缺陷会包含）。

- 终端交互使用：无变化（TTY 下仍彩色）。
- 已经在用 `sed`/`col -b` 等清洗 ANSI 的脚本：仍然正常（幂等操作）。
- 极少数依赖输出中含 ANSI 码的脚本（如 `grep $'\x1b\[32m'`）会失效——请移除该类过滤。
- 目前**没有**强制开色的途径（无 `FORCE_COLOR`/`--color=always`）；如需该能力请提 issue，已列入重构方案（docs/modules/internal-ui.md）。

## 3. NO_COLOR 环境变量（新增支持）

v0.3.0 遵循 [no-color.org](https://no-color.org/) 约定：`NO_COLOR` 为任意**非空**值即禁色（空值视为未设置）。
颜色判定优先级：`--no-color` flag > 配置文件 `no-color` > `NO_COLOR` env > 自动 TTY 检测。

如果你此前在环境里恰好设置了非空的 `NO_COLOR` 但依赖 dhs "无视它照常输出颜色"——那是 v0.2.0 的缺陷行为，请改用配置文件 `no-color: false` 之外的方式（即默认 TTY 彩色）。

## 4. DHS_API_BASE 镜像站使用者（重要）

`DHS_API_BASE` 现已正式文档化并有两项行为变化：

1. **尾斜杠自动去除**：`DHS_API_BASE=https://mirror.example.com/` 不再拼出 `//v2/...` 双斜杠路径（此前会，且常被反代 404）。
2. **分页游标必须留在同一 host**：若镜像站透传 Docker Hub 官方 API 返回的绝对 `next`（指向 `hub.docker.com`），v0.2.0 会静默跟随（第 2 页起流量绕过镜像直连官方）；v0.3.0 会**报错拒绝**：

   ```text
   tags next cursor https://hub.docker.com/v2/... points outside the configured base https://mirror.example.com; refusing to follow
   ```

   **镜像站修复方式**：把响应中的 `next`/`previous` 字段重写为指向镜像自身 host（保 path+query）。
   同 scheme、同 host（含 base 自带 userinfo）的游标不受影响。

## 5. 退出码与 usage 输出

- 运行期错误（网络失败、HTTP 错误、坏配置文件）的 stderr 输出**不再附带整段 usage 文本**，只有 `Error: ...` 行——解析 stderr 的脚本更干净了。错误退出码仍统一为 1。
- flag 语法错误（如 `--nope`）仍有自解释错误消息。

## 6. 配置文件（无变化，提醒）

`~/.config/dhs/config.yaml`（或平台等价路径）格式不变；v0.2.0 的配置无需迁移。
仍建议只用 `dhs config` 子命令读写（手写的注释与未知键在 set/unset 时会被丢弃——既有行为，v0.3.0 未变，已列入重构方案）。

## 7. Go 模块使用者

dhs 的 `internal/` 包受 Go internal 规则限制，外部模块本就无法 import；`SetEnabled` 语义变化（只禁不开）无外部影响面。

## 8. 产物/CI 相关

- Gitea Release 的 Linux 产物现在为静态链接（`CGO_ENABLED=0`）：从 Gitea 下载的二进制可直接跑在 Alpine/musl、scratch 容器（此前动态链接 glibc，在这些环境无法启动）。GitHub Release 产物无变化。
- 仓库新增 `.gitattributes`（Go 等文本文件强制 LF 检出）：Windows 开发者克隆后 `gofmt` 与 ubuntu CI 不再因 CRLF 误报，无需任何操作。
