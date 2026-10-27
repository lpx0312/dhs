# e2e 测试体系与 CI — 模块文档与重构方案

> 范围：`cmd/e2e_test.go`（进程内 CLI 级）、`e2e/e2e_test.go`（黑盒二进制级）、
> `.github/workflows/{ci,release}.yml`、`.gitea/workflows/release.yml`。

## 1. 三层测试体系

| 层 | 位置 | 测什么 | 环境 |
| --- | --- | --- | --- |
| L1 单元 | `internal/*/*_test.go`、`cmd/{search,tags,config,root}_test.go` | 函数级行为，httptest 直连 | 同进程，无 CLI 参与 |
| L2 进程内 CLI 级 | `cmd/e2e_test.go` | cobra 命令树端到端：flag 解析、配置读写、中文文案、错误分支、表格渲染 | 同进程执行 `rootCmd.Execute()`，mock hub + 捕获 stdout |
| L3 黑盒二进制级 | `e2e/e2e_test.go` | 真实二进制：退出码、子进程 env、跨平台构建产物 | `go build` 产物 + `exec.CommandContext` 子进程 |

**L2 基建**（cmd/e2e_test.go）：

- `resetFlags`（:20-26）：把包级 flag 变量手工恢复为定义默认值——cobra flag 值跨 Execute 持久，
  一个测试的 `--limit` 会泄漏进下一个。
- `captureStdout`（:30-50）：把 `os.Stdout` 换成 pipe 再跑 fn。CLI 全部 `fmt.Print*` 直写，
  cobra 的 `SetOut` 不起作用。
- `runCLI`（:54-67）：resetFlags + SetArgs + Execute + 捕获，Cleanup 里恢复全局态并
  `ui.SetEnabled(false)` 还原非 TTY 默认。
- `newTempConfig`（:70-72）隔离配置文件；`fixture`（:75-82）按相对路径
  `../internal/dockerhub/testdata` 读共享 mock payload；`newMockHub`（:86-108）起假 Docker Hub
  v2 API（未知路径 404），`t.Setenv` 注入 `DHS_API_BASE`、清空 `HTTPS_PROXY`/`HTTP_PROXY`。

**L3 基建**（e2e/e2e_test.go）：

- `TestMain`（:37-57）先 `go build -o $tmp/dhs-e2e` 构建真实二进制再跑测试；`os.Exit` 跳过
  defer，**临时目录在 `m.Run()` 后显式 `os.RemoveAll`**（:52-56，本轮修复——此前每次 `go test`
  泄漏一个 `dhs-e2e-*` 目录）。
- `run`（:69-92）用 `exec.CommandContext` + 30s 超时（本轮修复：无超时时卡死的子进程会挂起整个
  测试包），子进程 env 强制 `HTTPS_PROXY= HTTP_PROXY= NO_COLOR=1`（:74，本轮：NO_COLOR 让输出
  断言免于 ANSI 前缀），超时被杀时先打出已捕获输出再 Fatal（:79-82）。
- 退出码是 L3 独有断言面：未知命令必须精确退出 1（:136-144，root 的 `Execute()` 失败即
  `os.Exit(1)`，cmd/root.go:33-37）。

**串行约束**：L2 测试共享包级 `rootCmd`、flag 变量与全局 `os.Stdout`（captureStdout 直接换），
L1 的 dockerhub 测试改写包级 `tagsPageSleep`（internal/dockerhub/tags_test.go:421-426）。
Go 同包测试默认串行，因此这些测试**一律不得加 `t.Parallel()`**；跨包（L3 与 L2 分属不同包）
可以并行。

## 2. DHS_API_BASE 接缝贯穿三层

`dockerhub.BaseURL()` 是唯一的环境读取点（client.go:47-52，尾斜杠已 Trim）。三层各自如何用它
指向 mock：

- L1：不起作用——base 是 Fetch 函数的显式参数，直接传 `srv.URL`（search_test.go:30），
  不经环境变量；
- L2：`newMockHub` 用 `t.Setenv("DHS_API_BASE", srv.URL)`（cmd/e2e_test.go:103），同进程内
  `dockerhub.BaseURL()` 立即生效；部分测试手工起定制 server 后同样 `t.Setenv`（:319-321）；
- L3：`t.Setenv` 改的是测试进程环境，`run` 里 `os.Environ()` 展开后传给子进程
  （e2e/e2e_test.go:74、108），二进制内的 `BaseURL()` 读到 mock 地址。

同一个接缝同时服务两个用户：测试指向 mock、最终用户指向兼容镜像源。新增命令时保持
"所有 HTTP 出口都经 `dockerhub.BaseURL()`"这条不变量，mock 就永远够用。

## 3. CI 矩阵现状

- **ci.yml**（.github/workflows/ci.yml）：push master 与 PR 触发（:4-8），矩阵
  ubuntu-latest + windows-latest（:17），两个 job 都跑
  `go test ./... -coverprofile=cover.out -covermode=atomic`（:40）并打印覆盖率末行（:43）。
- **gofmt/vet 仅 ubuntu**（:28-37）：注释写明的理由是"Windows 检出可能带 CRLF，会误报 gofmt"
  （:28）。**本轮新增 `.gitattributes`（`*.go text eol=lf` 等）后 Windows 检出已被强制为 LF，
  该理由实质失效**——如实说明：条件现在可以移除让两平台都跑 gofmt；保留也无害（省 CI 分钟、
  行为不变），建议移除注释里过时的理由并二选一。
- **release.yml（GitHub）**：tag `v*` 触发（:5-7），6 目标交叉编译（linux/windows/darwin ×
  amd64/arm64，:30-35），`CGO_ENABLED=0 -ldflags "-s -w"` 静态产物，
  softprops/action-gh-release 上传。**构建前没有任何测试步骤**。
- **.gitea/workflows/release.yml**：Gitea 端对应物，5 目标（**缺 windows-arm64**，:29-33），
  `CGO_ENABLED=0` 已与 GitHub 端对齐（:28 注释明说），release 说明是硬编码中文 body（:43-55）。

## 4. 重构方案总览

| 编号 | 优先级 | 主题 | 工作量 |
| --- | --- | --- | --- |
| R1 | P2 | fixture 三份重复 | M |
| R2 | P2 | resetFlags 手工白名单 | S |
| R3 | P2 | captureStdout 不捕 stderr | S |
| R4 | P2 | CI 无 -race | XS |
| R5 | P2 | 覆盖率产物未上传 | XS |
| R6 | P2 | release 不跑测试直接发版 | S |
| R7 | P2 | 双远端 release 目标漂移 | S |
| R8 | P3 | 127.0.0.1:1 真实连接依赖 | XS |
| R9 | P3 | release 无 -trimpath/checksums | XS |

## 5. 重构项明细

### R1（P2）：fixture 三份重复

现状：同一份 mock Docker Hub 数据存在三处形态——

1. `internal/dockerhub/testdata/*.json`（唯一真源，3 个文件）；
2. `cmd/e2e_test.go:75-82` 的 `fixture()` 用相对路径 `..` 跨目录引用它（L2 依赖 L1 的测试
   数据目录，包布局耦合）；
3. `e2e/e2e_test.go:19-33` 的内联 const `searchBody`/`tagsBody`——注释自述"mirror the shared
   fixtures"，**且已经漂移**：search 2 条 vs 真源 3 条，tags 3 条 vs 真源 10 条，字段值也不同。

方案：建 `internal/testfixtures` 包用 `go:embed` 收敛，真源挪进该包，三层同源：

```go
// internal/testfixtures/testfixtures.go
package testfixtures

import _ "embed"

//go:embed search_nginx.json
var SearchNginx string

//go:embed tags_nginx_page1.json  // 同法暴露 TagsNginxPage1 / TagsNginxPage2
var TagsNginxPage1 string
```

L1 的 `os.ReadFile("testdata/...")`（search_test.go:78、tags_test.go:164）、L2 的 `fixture()`
与 L3 的内联 const 全部改引该包。注意 L3 内联数据已漂移，合并后断言数字（如"找到 2 个结果"
e2e_test.go:185、"共生成 4 条命令" :204）需按真源重算。工作量：M（主要是 L3 断言重算）。

### R2（P2）：resetFlags 手工白名单不自愈

现状：cmd/e2e_test.go:20-26 手工枚举每个包级 flag 变量。新增 flag 忘记登记时，前一个测试的
值静默泄漏到后续所有 runCLI——失败模式是"单独跑过、全量跑挂"，极难排查。

方案：改为遍历 cobra flag 集合自愈式复位：

```go
func resetFlags() {
    reset := func(fs *pflag.FlagSet) {
        fs.VisitAll(func(f *pflag.Flag) {
            _ = f.Value.Set(f.DefValue) // 恢复定义默认值
            f.Changed = false
        })
    }
    reset(rootCmd.PersistentFlags())
    reset(searchCmd.Flags())
    reset(tagsCmd.Flags())
}
```

也可在包级 `init` 后立即快照所有变量值、resetFlags 恢复快照（对非 flag 状态更稳）。
工作量：S；现有测试全部保持绿即验证成功。

### R3（P2）：captureStdout 不捕 stderr

现状：captureStdout（cmd/e2e_test.go:30-50）只换 `os.Stdout`。cobra 的用法/参数错误帮助文本
写 **stderr**，L2 断言拿不到（TestE2E_ArgumentCountErrors :263-278 只能断言 err 非 nil，无法
断言错误指引文本）。

方案：同时替换 `os.Stderr` 为第二支 pipe（两支 pipe 各起 goroutine ReadAll），
`runCLI` 返回 `stdout, stderr, err` 三元组。工作量：S——涉及全部 runCLI 调用点，
机械替换签名。

### R4（P2）：CI 无 -race

现状：ci.yml:40 只跑普通 `go test`。当前代码基本单线程，但 `captureStdout` 的 goroutine
（cmd/e2e_test.go:41-44）与 L3 子进程等待都是未来竞态高发区，race 检测几乎零成本。

方案：ubuntu job 加独立 step：`go test -race ./...`（race 需要 cgo，windows runner 上慢且
易出环境问题，只加 ubuntu）。工作量：XS。

### R5（P2）：覆盖率产物未上传

现状：ci.yml:40 生成 `cover.out`，:43 只打印 `go tool cover -func` 末行，PR 上看不到逐文件
覆盖率变化，产物随 job 一起丢弃。

方案：`actions/upload-artifact@v4` 上传 cover.out（两平台各一份）；若要 PR 评论可再加
codecov/shcoverage 类 action，但先从 artifact 起步即可。工作量：XS。

### R6（P2）：release 不跑测试直接发版

现状：`.github/workflows/release.yml` 与 `.gitea/workflows/release.yml` 都是 checkout →
交叉编译 → 发 Release，**全量测试只在 push master/PR 的 ci.yml 里跑**。直接在 master 上打
tag（或从非 master 提交打 tag）时，未测试代码直接变成公开二进制。

方案：两个 release workflow 在 build 前插入一步 `- name: Test before release` /
`run: go test ./...`。约 1-2 分钟换"tag 必经测试"的保证；更严格的做法（tag 指向已绿 CI 的
commit）对单人项目过重。工作量：S（两个文件各加一步）。

### R7（P2）：双远端 release 目标漂移

现状：GitHub 端 6 目标（含 windows-arm64，release.yml:30-35）vs Gitea 端 5 目标（gitea
release.yml:29-33），Gitea 缺 windows-arm64；release notes 一边 `generate_release_notes: true`
自动生成、一边硬编码中文 body（会随版本 stale）。`CGO_ENABLED=0` 本轮已在 Gitea 端对齐
（:28 注释）。

方案：GitHub Actions 与 Gitea Actions 语法兼容，可把编译矩阵抽成一份共享片段（如
`scripts/release-matrix.sh` 生成 build 命令，两个 workflow 各自调用），或最低限度在两文件
头部互相注明"修改矩阵时同步另一份"。建议前者。工作量：S。

### R8（P3）：127.0.0.1:1 真实连接依赖

现状：`TestE2E_SearchUnreachableProxyIsAnnotated`（cmd/e2e_test.go:409-419）与 tags 版
（:757-762）真的向 `http://127.0.0.1:1` 发起连接、期望立刻被拒。绝大多数环境端口 1 无监听、
内核立即 RST；但若跑在防火墙 DROP（而非 REJECT）策略的环境，请求会等到 30s client 超时
（client.go:38），两个测试拖慢 1 分钟。

方案：接受现状（本仓库 CI runner 与开发机均 REJECT 语义），在测试注释标注此前提；或改为
注入短超时 client 的 seam。不建议为测试改生产代码。工作量：XS（注释）。

### R9（P3）：release 无 -trimpath/checksums

现状：两个 release workflow 的 build 命令只有 `-ldflags "-s -w"`（release.yml:30-35），
无 `-trimpath`（产物内嵌构建机绝对路径，不可复现构建），也不生成 checksums 文件，用户无法
校验下载完整性。

方案：build 命令加 `-trimpath`；build 后加一步 `cd dist && sha256sum * > checksums.txt`，
并把 `dist/checksums.txt` 加进 release files 列表。工作量：XS。

## 6. 维护注意事项

- L2/L3 大量断言**中文文案**（"找到 3 个结果（共 3）"、"已写入"、"使用 --all 参数查看全部"），
  改动任何用户可见字符串前先 grep 测试，改文案必须同步钉测试。
- cmd/e2e_test.go:812-818 的注释仍把 `TestE2E_SearchNoColorEnvDisablesANSI` 标为
  "EXPECTED-RED"，但 `SetEnabled` 只禁不开的修复（internal/ui/color.go:30-34）落地后它已是
  绿色——下次触碰该文件时顺手把注释更新为【绿·钉修复】。
- e2e 包不含生产代码（doc.go:1-4），保持这个不变量：黑盒测试不得 import internal 包。
