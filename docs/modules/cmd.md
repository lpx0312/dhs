# cmd 模块文档与重构方案

> 对象：`cmd/` 包（cobra 命令层）。依赖方向：`main.go` → `cmd` → `internal/{config,dockerhub,ui}`，单向。
> 所有行号基于当前工作区代码，格式 `文件:行`。

## 1. 模块职责与文件清单

`cmd` 负责参数解析（cobra flags）、启动装配（配置→颜色→代理→HTTP 客户端）、调用 `internal/dockerhub` 取数、调用 `internal/ui` 渲染。不持有业务状态；唯一的"状态"是包级 flag 变量与 `osWriteFile` 接缝。

依赖方向（单向，cmd 位于最上层）：

```
main.go ──> cmd ──> internal/config   （配置读写、优先级链）
                ──> internal/dockerhub（HTTP 客户端、API 抓取与规范化）
                ──> internal/ui       （表格渲染、颜色包装）
```

| 文件 | 职责（一句话） |
| --- | --- |
| `cmd/root.go` | 根命令与持久 flags（--proxy/--config/--no-color，SilenceUsage 已开），共享 helper：`configPath`/`loadConfig`/`applyColorSetting`/`errWithProxy`/`shortDate`/`osWriteFile` 接缝（root.go:51-99） |
| `cmd/search.go` | `dhs search <keyword>`：搜仓库、渲染 RepoRow 表、页脚统计（search.go:14-75） |
| `cmd/tags.go` | `dhs tags <image>`：标签抓取-过滤-排序-渲染-export 流水线及全部纯函数 helper（tags.go:26-275） |
| `cmd/config.go` | `dhs config` 四个子命令 set/get/list/unset，键名规范化与值校验（config.go:14-205） |
| `cmd/root_test.go` 等 4 个 `*_test.go` | 单元测试 + RunE 直调测试（见 §6） |
| `cmd/e2e_test.go` | 进程内 e2e：`runCLI` 走 `rootCmd.Execute()` 全链路 + httptest mock（e2e_test.go:54-108） |

输入输出约定：所有面向用户的输出经 `fmt.Print*` 直写 stdout（e2e 靠换管捕获，见 §6）；面向脚本的裸值输出仅 `config get`（config.go:116）。

## 2. 命令体系表

| 命令 | flags（默认值） | 行为 |
| --- | --- | --- |
| `dhs`（root） | `--proxy/-P`("")、`--config`("")、`--no-color`(false)，均为 persistent（root.go:38-40）；rootCmd 已设 `SilenceUsage: true`（root.go:34，本轮修复，见 §8-R5） | 无子命令时打印 help |
| `search <keyword>` | `--limit/-n`(25)（search.go:65） | 单请求 `page_size=limit` 搜索，渲染 NAME/STARS/PULLS/OFFICIAL 表，页脚"找到 N 个结果（共 T）"（search.go:58） |
| `tags <image>` | `--limit/-n`(10)、`--sort`(date)、`--order`(desc)、`--all/-a`(false)、`--os`(false)、`--filter`("")、`--arch`("")、`--gen-pull`(false)、`--output/-o`("")（tags.go:135-143） | 完整流水线见 §4 |
| `config set <key> <value>` | 无 | 校验→Load→写回（config.go:86-111） |
| `config get <key>` | 无 | 裸值输出，未设置打印 `(未设置)`（config.go:129-138） |
| `config list` | 无 | 打印路径、文件值、生效代理与优先级说明（config.go:158-171） |
| `config unset <key>` | 无 | Load→置零值→写回（config.go:185-199） |

配置键只有 `proxy`、`no-color`（别名 `no_color`，config.go:18-26）。

flag 注册位置速查：root 持久 flags 在 root.go:37-41（init）；search flag 在 search.go:63-66（默认 limit=25 在 :65）；tags flags 在 tags.go:133-144。cobra 的 flag 值写进**包级变量**（root.go:14-18、search.go:12、tags.go:14-24），跨 Execute 调用残留，是测试必须 `resetFlags` 的根因（e2e_test.go:17-26）。

## 3. 启动序列与共享 helper

每个网络命令的 RunE 前奏固定四步：

```
loadConfig()                  // --config 或平台默认路径，缺文件=零值不报错（root.go:59-65）
applyColorSetting(cfg)        // 只在需要禁色时 SetEnabled(false)，从不强制开色（root.go:70-74）
proxy := cfg.EffectiveProxy(flagProxy, "")   // flag > file > HTTPS_PROXY > HTTP_PROXY（config/config.go:41-55）
client, err := dockerhub.NewClient(proxy)    // 空 proxy → ProxyFromEnvironment（dockerhub/client.go:18-40）
```

完整四步出现在 search.go:24-34 与 tags.go:41-51；config 四个子命令只做前两步（config.go:91-99/124-128/148-156/185-193），`config list` 额外调 `EffectiveProxy` 展示生效值（config.go:169）。API 地址由 `dockerhub.BaseURL()` 解析，`DHS_API_BASE` 环境变量可覆盖（client.go:47-52，测试注入点）。

各步失败时的行为：

| 步骤 | 失败形态 | 用户所见 |
| --- | --- | --- |
| loadConfig | 默认路径解析失败 / 坏 YAML | `resolving default config path: …`（root.go:51）/ 原样 yaml 错误（不带路径，见 internal-config.md §4-R7） |
| NewClient | proxy URL 无 scheme / 解析失败 | `invalid proxy URL …`（client.go:27-31），e2e 钉在 e2e_test.go:538-544 |
| Fetch* | HTTP 非 2xx / 坏 JSON / 网络失败 | `HTTP %d` / `parsing … response` / 经 `errWithProxy` 附代理标注 |

重复问题本身（三处逐行复制）见 §8-R3 的 PersistentPreRunE 方案。

## 4. tags 流水线详解

RunE（tags.go:37-130）按序执行，各阶段速查：

| # | 阶段 | 调用处 | 定义 | 备注 |
| --- | --- | --- | --- | --- |
| 1 | parseImageRef | tags.go:39 | tags.go:148-153 | 第一个 `/` 切分，无斜杠补 `library`；已知误切见 §8-R1 |
| 2 | 启动四步 | tags.go:41-51 | §3 | — |
| 3 | limit 解析 | tags.go:55-61 | — | `--all`→0（抓全部）；`tagsLimit<=0`→10；limit 作为**抓取上限**传给 FetchTags |
| 4 | FetchTags | tags.go:62 | dockerhub/tags.go:24-81 | page_size=100 翻页；丢弃 API `count`（dockerhub/tags.go:80） |
| 5 | parseArchFlag | tags.go:67 | tags.go:156-169 | `amd64,arm64`→切片；空串→nil |
| 6 | filterByName | tags.go:70-72 | tags.go:171-179 | 子串、大小写敏感 |
| 7 | filterByArch | tags.go:73-75 | tags.go:181-196 | 多目标 OR；空架构集永不匹配 |
| 8 | sortTags | tags.go:76 | tags.go:198-216 | 稳定排序；未知键落 date；仅精确 `asc` 升序 |
| 9 | display 截断 | tags.go:77-80 | — | 非 `--all` 时 `tags[:tagsLimit]` |
| 10 | render | tags.go:83-105 | tags.go:218-230 | 计数行 + `ui.RenderTagTable` + `--all` 提示 |
| 11 | gen-pull | tags.go:108-120 | tags.go:232-246 | 必须 `--arch`，否则黄字忽略 |
| 12 | export | tags.go:123-128 | tags.go:250-275 | `osWriteFile` 接缝；错误包 `writing output file` |

抓取细节（dockerhub/tags.go）：跟随 `next` 游标，相对游标对 base 解析、绝对游标必须同域（:87-103），已访问 URL 拒绝以防翻页死循环（:31-33），页间 `tagsPageSleep` 500ms 限速（:14/78），`len(out)>=limit` 即停（:70-72）。

### 警告：`--limit` 语义 = 抓取量上限，不是"过滤后取 N 条"

`--limit` 在过滤/排序**之前**就限制了 FetchTags 的抓取量，且抓取按 100/页 的页粒度生效：limit≤100 恰好抓一页（最多 100 条）。因此：

- `--filter`/`--arch`/`--sort` 只作用于已抓到的集合；目标标签若在第 101 个之后，会"不存在"。
- 计数行"找到 N 个标签"的 N 是**已抓集合过滤后的条数**，不是仓库标签总数（真实总数在被丢弃的 `count` 字段里）。测试夹具即是证据：`internal/dockerhub/testdata/tags_nginx_page1.json` 声明 `"count": 12` 但本页只有 10 条，默认运行打印"找到 10 个标签"（e2e_test.go:616-638 钉住的正是这个现状）。
- 需要完整过滤/真实总数时必须 `--all`。

## 5. 错误处理约定

- **包装**：`errWithProxy` 用 `%w` 追加"（当前代理: …）"，`errors.Is` 链保持完整（root.go:76-90，测试 root_test.go:172-190）。proxy 为空时原样透传（root_test.go:165-170）。**脱敏已于本轮修复**：打印前 `url.Parse` 成功则 `URL.Redacted()`——`user:pass@host` 形态在错误文本中呈 `user:xxxxx@host`，明文密码不进 stderr/日志；解析失败则原样打印。绿钉：root_test.go:192-211（带凭据单测）、e2e_test.go:525-536（带凭据死代理 e2e）。
- **已知怪癖**：`errWithProxy(nil, proxy)` 不短路，产出带 `%!w(<nil>)` 的非 nil 错误，已被绿钉（root_test.go:213-227）。当前所有调用点都在 `err != nil` 分支内（search.go:39、tags.go:64），怪癖暂无触发路径。
- **退出码**：`Execute` 对任何错误统一 `os.Exit(1)`（root.go:44-48），配置错/网络错/参数错不做区分。黑盒 e2e 依赖此约定校验退出码（e2e/e2e_test.go 的 `run` 返回 exit code）。
- **写文件错误**：export 失败包一层 `writing output file: %w`（tags.go:125），其余错误不再二次包装。
- **SilenceUsage 已设**（root.go:34，本轮修复）：RunE 运行期错误（如 HTTP 429）只打印 "Error:" 行，不再倾倒整段 usage（e2e 钉在 e2e_test.go:327-352）。cobra v1.10.2 实测：flag 语法错误与参数个数错误的 usage 同样被抑制（cobra 在 ExecuteC 对 execute() 返回的所有错误——含 FlagErrorFunc 包装的 flag 错误——统一检查 SilenceUsage），但 "Error:" 消息本身自解释（如 `unknown flag: --nope`），见 e2e_test.go:354-380。

## 6. 测试指南（三层 + 接缝）

1. **单元层**（`root_test.go`/`config_test.go`/`search_test.go`/`tags_test.go`）：直调纯函数与 helper；`config_test.go:191-334` 直调四个 RunE（绕过 cobra flag 解析，直接改包级变量）。需要构造带 `archSet` 的 Tag 时必须经 `FetchTags`+httptest mock（`fetchTagsFromMock`，tags_test.go:19-30，archSet 未导出）。
2. **进程内 e2e**（`cmd/e2e_test.go`）：`runCLI` 复位 flags → `rootCmd.SetArgs` → 捕获 stdout 执行 `rootCmd.Execute()`（:97-110）；`runCLIStderr`/`captureStderr` 是 stderr 版（:55-95），用于断言 SilenceUsage 下用户实际看到的错误通道输出；`newMockHub` 用共享 fixture + `DHS_API_BASE` + 清空代理 env 造离线 Docker Hub（:129-151）。
3. **黑盒 e2e**（`e2e/e2e_test.go`）：`TestMain` 真实 `go build` 出二进制再 `exec.Command` 运行，校验退出码与输出。

**接缝清单**：`resetFlags`（e2e_test.go:20-26，cobra flag 值跨 Execute 残留）、`captureStdout`/`captureStderr`/`captureRunEOutput`（e2e_test.go:30-78 / config_test.go:162-182，CLI 直接 `fmt.Print*`，`SetOut` 不够用）、`osWriteFile`（root.go:99，变量替换模拟写失败）、`tagsPageSleep`（dockerhub/tags.go:14）、`DHS_API_BASE`（client.go:47-48）。

共享夹具位于 `internal/dockerhub/testdata/`：`search_nginx.json`（count=3）、`tags_nginx_page1.json`（count=12、本页 10 条）、`tags_nginx_page2.json`（count=12、2 条）——page1 的 count 与条数差是 §8-R2"已抓数≠总数"的活证据，`newMockHub` 默认只挂 page1（e2e_test.go:129-151），跨页用例自行改写 `next` 游标（e2e_test.go:647-692）。

**禁止 `t.Parallel()` 的原因**：cmd 包的全部 flag 是包级变量（root.go:14-18、search.go:12、tags.go:14-24），测试靠快照+`t.Cleanup` 恢复；`captureStdout` 换全局 `os.Stdout`；`applyColorSetting` 翻转 fatih/color 全局开关（e2e_test.go:61、config_test.go:186-188 统一在 cleanup 复位）；且大量使用 `t.Setenv`（本身禁并行）。并行化必须先消除这些全局量。

常用验证命令：

```bash
go test ./cmd/...              # 单元 + 进程内 e2e（httptest mock，离线安全）
go test ./e2e/...              # 黑盒 e2e（TestMain 先 go build 真实二进制）
go test ./... && go vet ./...  # 全量回归
```

## 7. 近期变更（本轮已修复）

- **rootCmd 设 `SilenceUsage: true`（本轮修复，§8-R5）**：RunE 运行期错误（网络/HTTP/坏 YAML）不再在 "Error:" 行后倾倒整段 usage，脚本消费 stderr 不再被噪音淹没。cobra v1.10.2 实测：flag 语法错误与参数个数错误的 usage 同样被抑制（ExecuteC 对 execute() 返回的所有错误统一检查 SilenceUsage），"Error:" 消息本身自解释。绿钉：e2e_test.go:327-352（TestE2E_RuntimeErrorNoUsageDump / TestE2E_FlagErrorStillExplainsItself）。
- **errWithProxy 代理 URL 脱敏（本轮修复，§8-R6）**：打印前 `url.Parse` 成功则 `URL.Redacted()`——`user:pass@host` 在错误文本中呈 `user:xxxxx@host`，明文密码不泄漏到 stderr/日志；解析失败原样打印（root.go:76-90）。绿钉：root_test.go:192-211、e2e_test.go:525-536。
- **applyColorSetting/SetEnabled 不再强制开色**：修复前 `SetEnabled(!EffectiveNoColor(...))` 在无需禁色时调 `SetEnabled(true)`，把 `color.NoColor` 强写为 false，破坏 fatih/color 的 NO_COLOR/非 TTY 自动检测。现在 `applyColorSetting` 只在禁色时调 `SetEnabled(false)`（root.go:70-74），`ui.SetEnabled` 同步改为"只禁不开"（ui/color.go:30-34）。绿钉：root_test.go:116-161、e2e_test.go:935-945（原 EXPECTED-RED 已转绿）。
- **NO_COLOR env 层级补齐**：`EffectiveNoColor` 新增第三层 `NO_COLOR` 非空即禁色，最终优先级 `--no-color` flag > 配置文件 `no_color` > `NO_COLOR` env（config/config.go:61-72）。绿钉：config/config_test.go:260-302。

## 8. 重构方案（按优先级排序）

### R1（P1）parseImageRef 对 tag/digest/registry 引用误切

- **现状证据**：tags.go:148-153 按第一个 `/` 盲切。`nginx:1.27` → name=`nginx:1.27`（tag 未剥离，查询 404）；`nginx@sha256:…` 同样整体进 name；`registry.io:5000/team/app` → user=`registry.io:5000`、name=`team/app`（host:port 被当仓库名）。已绿钉：tags_test.go:56-68（四个畸形用例，修复落地时期望值需翻转）。
- **方案**：首段（首个 `/` 之前）含 `.`、`:`、`localhost`（端口或域名特征）时判定为 registry 前缀——dhs 只支持 Docker Hub，直接报错"暂不支持第三方 registry"；引用中含 `@digest` 报"digest 引用暂不支持，请用 tag 名"；含 `:tag` 时给出专属提示（剥离 tag 或提示去掉）。
```go
func parseImageRef(ref string) (string, string, error) {
    first := ref
    if i := strings.Index(ref, "/"); i >= 0 { first = ref[:i] }
    if strings.ContainsAny(first, ".:") || first == "localhost" {
        return "", "", fmt.Errorf("检测到 registry 前缀 %q：dhs 仅支持 Docker Hub，请去掉后重试", first)
    }
    if i := strings.Index(ref, "@"); i >= 0 {
        return "", "", fmt.Errorf("不支持 digest 引用（%s…）：请改用 tag 名", ref[:min(i+13, len(ref))])
    }
    // :tag 提示 / 原有 user/image 切分……
}
```
- **影响面**：tags.go:39 调用点改签名；tags_test.go:43-75 钉住用例翻转；e2e 无涉及这些引用形态的用例。**工作量**：0.5 天（含测试翻转）。

### R2（P1）search --limit 无上限 + tags 把已抓数当总数

- **现状证据**：search 的 limit 原样透传为 `page_size`（search.go:20-22 → dockerhub/search.go:19-20），真实 API 静默钳到 100，用户 `--limit 500` 得 100 且无提示；透传行为已绿钉 e2e_test.go:376-394。tags 的"找到 N 个标签"用 `len(tags)`（tags.go:89/97），而 API 总数字段 `count` 在 FetchTags 返回时被丢弃（dockerhub/tags.go:80，types.go:21）；fixture 即反例（count=12、单页 10 条），现状被 e2e_test.go:499-521 钉住。
- **方案**：(a) 定义常量 `maxSearchPageSize = 100`，RunE 或 FetchRepositories 内 clamp，超出时黄字提示"已按 API 上限 100 收窄"；(b) `FetchTags` 改返回 `([]Tag, int, error)`，把首页 `pr.Count` 透传出来，tags.go 计数行改用 API 总数（`--all` 时 `len(tags)` 与 count 不一致也可提示）；翻页累计数另行展示。
```go
total := 0
for {
    // ...json.Unmarshal...
    if total == 0 { total = pr.Count } // 首页 count 即全量总数
}
return out, total, nil
// tags.go: fmt.Printf("找到 %d 个标签（仓库共 %d 个）…", len(tags), total)
```
- **影响面**：dockerhub/tags.go 签名变化波及 tags_test.go:25 与 e2e 断言文案（e2e_test.go:508/516/556 等的"找到 N 个"需同步改）；search 侧改 flag 校验即可。**工作量**：1 天。

### R3（P2）RunE 启动序列三处重复

- **现状证据**：search.go:24-34、tags.go:41-51 逐行重复 loadConfig→applyColorSetting→EffectiveProxy→NewClient；config 四个子命令又各自重复 loadConfig+applyColorSetting（config.go:95-99/124-128/148-156/185-193）。无测试钉住结构（e2e 只钉行为）。
- **方案**：rootCmd 加 `PersistentPreRunE` 统一做 loadConfig+applyColorSetting（颜色对 help/补全无害），网络命令再用小组合 helper 取 client：
```go
func newHubClient(cfg *config.Config) (*dockerhub.Client, string, error) // EffectiveProxy+NewClient
// search.go RunE: cfg, client, proxy, err := bootHubClient()
```
- **影响面**：仅 cmd 包内部；e2e 全绿即验收。**工作量**：0.5 天。

### R4（P2）config 键名分派散布 7 处

- **现状证据**：新增一个配置键要同步改 ①`configKeys`（config.go:15）②`normalizeKey` switch（:18-26）③`applySetting` switch（:29-51）④`clearSetting` switch（:54-63）⑤`config get` RunE switch（:129-138）⑥`config list` 的 printf 行（:166-167）⑦`configCmd.Long` 文档（:70-72）。行为被 TestNormalizeKey/TestApplySetting/TestClearSetting 钉住（结构可自由重构）。
- **方案**：表驱动 `keySpec`，一处声明键名/别名/校验/取值/清零/描述：
```go
type keySpec struct {
    key      string
    aliases  []string
    validate func(string) (any, error) // 返回写入 Config 的新值
    get      func(*config.Config) string
    clear    func(*config.Config) *config.Config
    desc     string // 喂 Long 文档与补全
}
var keySpecs = []keySpec{{key: "proxy", ...}, {key: "no-color", aliases: []string{"no_color"}, ...}}
```
- **影响面**：config.go 全文件；现有单测与 e2e 文案断言不变即可通过。**工作量**：0.5 天。

### R5（P2）SilenceUsage 未设 —— 已于本轮修复

- **状态（本轮）**：已落地 `rootCmd.SilenceUsage = true`（root.go:34），并有绿钉：e2e_test.go:323-346（运行期错误不再倾倒 usage）、e2e_test.go:348-377（flag 语法错误路径实测）。详见 §5 与 §7。
- **现状证据（修复前）**：root.go:20-24 未设置；任何 RunE 错误（含网络失败、坏 YAML）都会连带打印整段 usage，干扰脚本消费 stderr。无测试钉住（runCLI 只断言 err 文本）。
- **方案（按此实施）**：`rootCmd.SilenceUsage = true`（usage 错误由 cobra 在 flag 解析失败时另走 `FlagErrorFunc`，或对参数个数错误单独保留 usage）。**影响面**：一行 + 1 个 e2e 用例（断言 429 错误不再伴随 "Usage:"）。**工作量**：0.5 小时。

### R6（P2）errWithProxy 打印可能含凭据的代理 URL —— 已于本轮修复

- **状态（本轮）**：已按下方方案落地（root.go:76-88），打印前 `url.Parse` 成功即 `URL.Redacted()`；带凭据形态有绿钉：root_test.go:192-208、e2e_test.go:522-536。详见 §5 与 §7。
- **现状证据（修复前）**：root.go:70 把 proxy 原样拼进错误文本；`http://user:pass@host:port` 形态的凭据会直接暴露在终端/日志。URL 原文出现行为已被 root_test.go:178 与 e2e_test.go:416/759 钉住（这些用例不含凭据，改用 Redacted 后仍绿）。
- **方案**：
```go
func errWithProxy(err error, proxy string) error {
    if proxy == "" { return err }
    if u, e := url.Parse(proxy); e == nil {
        proxy = u.Redacted() // user:pass → user:xxxxx
    }
    return fmt.Errorf("%w\n（当前代理: %s）", err, proxy)
}
```
- **影响面**：root.go 单函数；补一条含凭据 proxy 的单测断言输出为 `user:xxxxx@`。**工作量**：0.5 小时。

### R7（P2）exportTags 与 renderTagRows 两套格式化

- **现状证据**：同一组单元格格式各写一遍：renderTagRows 的 TagRow 组装（tags.go:221-227）vs exportTags 的 markdown 行（:261-262），Size `%.1f`、Arches join、shortDate 三处规则重复，将来改口径必漏一边。内容分别被 TestRenderTagRows（tags_test.go:203-223）与 TestExportTags_MarkdownContent（:245-274）钉住（行为不变即安全）。
- **方案**：抽公共单元格函数，两个渲染端只管布局：
```go
func formatTagCells(t dockerhub.Tag) (tag, status, size, arch, updated string) {
    return t.Name, t.Status, strconv.FormatFloat(t.SizeMB, 'f', 1, 64),
        strings.Join(t.Arches, ", "), shortDate(t.LastUpdated)
}
```
- **影响面**：tags.go 两个函数内部；现有单测不动。**工作量**：1 小时。

### R8（P3）--sort/--order 非法值静默回退

- **现状证据**：`sortTags` 未知键落 date 分支（tags.go:207-208）；`tagsOrder == "asc"` 之外一切值当 desc（tags.go:76）。均已绿钉：tags_test.go:348-369、tags_test.go:383-440。
- **方案**：flag 注册改 `cobra.NoOptDefVal` 无关，直接在 RunE 前校验：非法值报错退出并列出合法值；或注册自定义 `flag.Value`。钉住测试需翻转。**工作量**：2 小时。

### R9（P3）normalizeKey 不归一化大小写/空白

- **现状证据**：config.go:18-26 精确匹配；`"Proxy"`、`" proxy"` 报"未知的配置项"。已绿钉 config_test.go:93-105。
- **方案**：`normalizeKey` 内 `strings.ToLower(strings.TrimSpace(s))` 再匹配（键名本身全小写无歧义）。**工作量**：0.5 小时（含钉住测试翻转）。

### R10（P3）config 子命令补全不一致

- **现状证据**：`get`/`unset` 有 `ValidArgsFunction`（config.go:118/179），`set` 没有（:82-85）——`dhs config set <TAB>` 无键名补全；`configKeys` 又与 R4 的表重复。无测试钉住。
- **方案**：随 R4 落地：`set` 的 ValidArgsFunction 对第 1 个参数补键名、第 2 个参数按 keySpec 给值建议（如 no-color → true/false）。**工作量**：1 小时。

### 钉住状态速查

| 重构项 | 现状是否已被测试钉住 | 钉住位置 |
| --- | --- | --- |
| R1 parseImageRef | 是（绿钉畸形用例） | tags_test.go:56-68 |
| R2 search limit 透传 / tags 计数 | 是 / 是（文案级） | e2e_test.go:376-394 / e2e_test.go:499-521 |
| R3 启动序列重复 | 否（行为有 e2e 网） | — |
| R4 键名分派 | 行为有、结构无 | config_test.go:14-88 |
| R5 SilenceUsage | 是（本轮修复后钉住） | e2e_test.go:323-377 |
| R6 errWithProxy 原文 URL | 是（无凭据 + 本轮补带凭据脱敏形态） | root_test.go:172-208、e2e_test.go:508-536 |
| R7 双套格式化 | 行为有、结构无 | tags_test.go:203-274 |
| R8 sort/order 回退 | 是（两处绿钉） | tags_test.go:348-369、383-440 |
| R9 normalizeKey | 是 | config_test.go:93-105 |
| R10 补全不一致 | 否 | — |

## 9. 附：维护者上手清单

改动 cmd 层时的最小检查集：

1. 改任何 RunE 行为 → 先跑 `go test ./cmd/...`，e2e 文案断言（"找到 N 个"/"当前代理"等）是最常见的破裂点。
2. 改 helper 签名（parseImageRef/sortTags/exportTags…）→ 同步翻转 tags_test.go 中的【绿·钉住现状】用例，注释里已写明翻转时机。
3. 新增配置键 → 按 §8-R4 的 7 处清单逐一排查（表驱动落地前仍是手工同步）。
4. 新增 flag → 记得把它加进 `resetFlags`（e2e_test.go:20-26），否则跨用例残留。
