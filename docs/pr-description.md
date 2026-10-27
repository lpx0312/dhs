# PR 描述：全仓代码审查驱动的质量收敛（v0.3.0）

> 以下内容可直接粘贴到 GitHub/Gitea PR 描述框。

---

## Title

```
fix: 全仓审查驱动的质量收敛——颜色约定/分页游标加固/凭据脱敏 + 240 测试与模块文档
```

## Summary

一次系统性的质量收敛：按目录拆 10 个并行代码审查任务，用约 100 个新增测试钉住全部发现，经两轮多角色（安全 / Go 实现 / 测试质量 / CLI 体验）交叉 review 后修复。**+2322/−136，25 个文件；测试 143 → 240（全绿，覆盖率 97.6%）；新增 5 份模块文档与重构方案、CHANGELOG 与迁移指南。**

## 问题发现（为什么改）

首轮审查 + 多角色 review 交叉确认的问题（均已修复并配回归测试）：

| 类别 | 问题 | 严重度 |
|------|------|--------|
| 颜色 | `SetEnabled(true)` 无条件覆写 fatih/color 检测：`NO_COLOR=1` 被无视、管道输出泄漏 ANSI 码 | P1 |
| 网络 | 分页 `next` 游标盲跟：镜像站场景第 2 页流量穿透回真实 Docker Hub；相对路径游标直接报错；协议相对 URL（`//evil/…`）可绕过 host 校验（review 实测复现） | P1 |
| 网络 | 分页无环保护/无页数上限，`--all` 可死循环 | P1 |
| 网络 | `user`/`image` 路径段未转义（`a#b` 的 `#` 截断 URL） | P1 |
| 安全 | 错误输出原样打印代理 URL 与请求 URL——`http://user:pass@proxy` 形态的凭据泄漏到 stderr/CI 日志 | P1/P2 |
| 命令 | `SilenceUsage` 未设：任何运行期错误后倾倒整段 usage 淹没真实错误 | P2 |
| 渲染 | 表格列宽把 ANSI 字节计入宽度（彩色/无色模式列错位）；重复 tag 排序不稳定 | P2 |
| 网络 | `DHS_API_BASE` 尾斜杠拼出 `//v2/...` 双斜杠 URL | P2 |
| 基建 | e2e `TestMain` 的 `defer os.RemoveAll` 在 `os.Exit` 下永不执行（TEMP 泄漏）；子进程无超时保护 | P1/P2 |
| 构建 | Gitea release 产物缺 `CGO_ENABLED=0`（动态链接 glibc，Alpine/scratch 起不来）；`go.mod` direct 依赖全标 `// indirect`；无 `.gitattributes`（CRLF vs CI gofmt） | P2 |

## 主要变更

**修复**
- 颜色链重做：`SetEnabled` 只禁不开；优先级 `--no-color` > 配置 > `NO_COLOR` env（no-color.org 约定）> TTY 自动检测。
- `resolveNext` 游标处理：相对路径解析、归一化后统一校验 scheme+host+userinfo（拒绝协议相对绕过、异 scheme 降级、注入 userinfo）、环保护 + `maxPages=1000` 上限；URL 一律 `url.PathEscape`；`BaseURL()` 去尾斜杠。
- 凭据脱敏：`errWithProxy` 与所有带 URL 的错误消息经 `Redacted()`（`user:xxxxx@host`）。
- `SilenceUsage: true`；表格宽度计量剥离 ANSI；`FormatPullCommands` 稳定排序；e2e TEMP 清理与 30s 子进程超时。

**新增**
- `NO_COLOR` 支持；`DHS_API_BASE` 正式文档化（含镜像站游标同 host 约束）。
- `docs/modules/` 5 份模块文档（职责/数据流/测试指南/带优先级重构方案）；`CHANGELOG.md`；`docs/migration-guide.md`。
- `.gitattributes`（LF 归一）；Gitea release `CGO_ENABLED=0`。

**测试**
- 143 → 240 个测试（全离线 httptest）；覆盖率 97.6%（ui 包 100%）；`-shuffle` 乱序通过；
钉现状/钉修复注释约定，重要缺陷全部有回归护栏（如 `externalHits == 0`、脱敏形态断言）。

## 测试与验证

```text
go test ./... -count=1            全部 ok（cmd / e2e / config / dockerhub / ui）
go test ./... -shuffle=on         全部 ok
go vet ./... / gofmt -l .         干净
覆盖率（-covermode=atomic）        cmd 96.7% · config 97.5% · dockerhub 96.2% · ui 100%
```

修复过程遵循测试先行：12 个"预期红"测试先钉住缺陷（含 review 阶段实测复现的协议相对 URL 绕过），修复后全部转绿，无测试断言被弱化放行。

## 迁移注意（详见 `docs/migration-guide.md`）

1. HTTP 错误消息末尾新增 URL 后缀（前缀匹配的脚本不受影响）。
2. 管道/重定向输出不再含 ANSI 码（此前为缺陷行为）；无强制开色途径。
3. `DHS_API_BASE` 镜像站：透传官方绝对 `next` 游标的镜像需将其重写为自身 host，否则 v0.3.0 报错拒绝（防流量穿透）。
4. 运行期错误的 stderr 不再附带 usage 文本；退出码不变（1）。
5. Gitea Linux 产物改为静态链接，Alpine/scratch 可直接运行。

## 已知未修（列入 `docs/modules/*` 重构方案，按用户痛感排序）

`nginx:1.27` 输入误 404（应提示去 tag 后缀）、tags "找到 N 个标签"以已抓数当总数、`--arch 386/arm --gen-pull` 静默 0 条、`search --limit >100` 静默截断、配置注释被抹、Save 非原子写、CJK 列宽、fixture 三份收敛、CI `-race`/覆盖率上传等。

## 提交建议

- 单 commit 或按 fix(颜色/网络/命令/基建)+test+docs 拆分均可；CI（ubuntu+windows 矩阵）预期全绿。
- 合入后打 `v0.3.0` tag 将同时触发 GitHub 与 Gitea 的 release 流水线（Gitea 产物自本版本起静态链接）。
