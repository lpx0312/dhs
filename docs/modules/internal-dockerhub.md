# internal/dockerhub — 模块文档与重构方案

> 范围：`internal/dockerhub/{client,search,tags,types}.go` 及配套测试。
> 定位：Docker Hub Hub API v2 的纯客户端包——HTTP + JSON 解析 + 归一化，不含参数解析、配置与终端输出。

## 1. 职责与设计

分层 `main → cmd → internal/{config,dockerhub,ui}` 中，本包只负责"发请求、收 JSON、产出归一化结构体"。
参数/配置/上色/表格全部留在 `cmd` 与 `internal/ui`，因此本包不 import cobra、config、ui。

设计要点（有意为之，改动前先理解动机）：

- **函数式风格，无自定义 Client 结构**。`NewClient(proxyURL)`（client.go:18-40）只产出标准
  `*http.Client`（自定义 Transport + 30s 总超时），所有 Fetch 函数把它当第一个参数收进来。
  没有 `type Client struct{...}`，无接收器藏状态问题；测试里 `srv.Client()` 可原样注入（search_test.go:29）。
- **base 作显式参数**。`FetchRepositories(client, base, keyword, limit)`（search.go:15）与
  `FetchTags(client, base, user, image, limit, showOS)`（tags.go:31）都要求调用方传 base URL，
  单元测试不必设置环境变量即可指向 httptest 服务器。
- **`BaseURL()` 是唯一的环境接缝**（client.go:47-52）：读 `DHS_API_BASE`，否则用包内常量
  `baseURL = "https://hub.docker.com"`（client.go:12）。cmd 层在调 Fetch 前调 `dockerhub.BaseURL()`
  （cmd/search.go:37、cmd/tags.go:62），e2e/黑盒测试靠这个变量把 CLI 指向 mock（见 e2e-and-ci.md）。
- **代理语义**（client.go:14-17 注释）：`proxyURL` 为空走 `ProxyFromEnvironment`，非空则显式
  `ProxyURL(u)` 且忽略环境变量，避免两套代理叠加。优先级链 `--proxy > 配置文件 > 环境变量 > 直连`
  由 `config.EffectiveProxy`（internal/config/config.go:41-55）在 cmd 层拼接。

## 2. API 级端点与分页语义

| 端点 | 函数 | 分页 | limit 语义 |
| --- | --- | --- | --- |
| `GET {base}/v2/search/repositories/?query=<kw>&page_size=<limit>` | `FetchRepositories`（search.go:15-53） | **单页**：搜索是排序结果，取 top-N 即可 | 即 `page_size`，`<=0` 归一为 25（search.go:16-18）；API 实际上限 100，本包**透传不钳制**（search_test.go:194-203 钉住），钳制责任在 cmd 层 |
| `GET {base}/v2/repositories/{user}/{image}/tags/?page_size=100` | `FetchTags`（tags.go:31-93） | **跟随 next 游标**翻页，页大小固定 100，总页数受 `maxPages` 硬上限 | `limit=0` 抓全部页；`limit>0` 是"抓取量上限"——收满即停（tags.go:82-84），**不裁剪**：一页 100 条全收，`limit=101` 会返回 200 条（tags_test.go:598-628 钉边界） |

细节：关键词 `url.QueryEscape`（search.go:19，search_test.go:121-130）；`user`/`image` 各自
`url.PathEscape`（tags.go:33），`image="a#b"` 传输为 `%23`，`page_size` 不沦为 fragment
（tags_test.go:494-512）；页间 `tagsPageSleep = 500ms`（tags.go:14，测试可置 0，tags_test.go:422-426）；
翻页总数受 `maxPages = 1000` 硬上限（tags.go:20，测试可改小），防"每页唯一游标"式无限翻页；
404 有专属文案 `image %s/%s not found`（tags.go:51-53），其余非 2xx 统一 `HTTP %d for %s`，
其中 URL 一律经 `redactURL`（tags.go:134-142，`u.Redacted()`）脱敏后再打印，base/游标
携带的 userinfo 密码不泄漏到终端。

## 3. 类型边界：非导出原始结构 vs 导出归一化结构

两层类型（types.go）：

- **非导出原始 JSON 结构**：`imageInfo`/`tagResult`/`paginatedTags`/`repoResult`/`paginatedRepos`
  （types.go:4-43），字段名与 API JSON 一一对应。注意 search API 用 `repo_name` 而非 `name`，
  且不返回 `last_updated`（types.go:28-29 注释）。
- **导出归一化结构**：`Tag`（types.go:48-56）与 `Repo`（types.go:59-66），字段经过换算
  （`FullSize` 字节 → `SizeMB`，tags.go:73）、拼接（`Arches` 列表）与过滤（linux-only）。

`Tag.archSet` 是包内私有的 `map[string]bool`（types.go:55），只装 **linux 架构字段（剥掉 variant）**
（tags.go:168-176），对外只暴露 `HasArch(target)`（tags.go:186-188）。封装的收益是 cmd 无法绕过
归一化摸原始数据；代价是 **cmd 层测试无法字面构造带 archSet 的 `Tag`**——`filterByArch`/
`buildPullSpecs` 的 cmd 级测试必须先起 mock hub 走 `FetchTags` 拿真 Tag（cmd/e2e_test.go:609-633），
构造成本高。空集语义（任何 `--arch` 都不匹配）只有测试钉住，godoc 没写，见 R6。

## 4. 近期变更（本轮已修复，文档与代码同步）

1. **`BaseURL()` 去尾斜杠**（client.go:49）：`DHS_API_BASE="http://srv/"` 返回 `http://srv`，
   拼接 `base + "/v2/..."` 不再产出 `//v2/...` 双斜杠（client_test.go:86-91；
   search_test.go:243-258 钉的是 Fetch 层直接传带尾斜杠 base 的行为，仍成立）。
2. **next 游标相对路径解析**：`resolveNext`（tags.go:104-121）把相对游标 `ResolveReference`
   到 base，修复相对 next 直接 `client.Get` 报 `unsupported protocol scheme ""`
   （tags_test.go:433-459）。**已于第二轮安全审查加固**：ResolveReference 的结果同样
   过 scheme/host/userinfo 校验，协议相对 URL（`//evil/...`）不再借 `Scheme==""` 分支穿透。
3. **next 游标越出 base 拒绝**：绝对 URL 指向 base 之外（如被劫持镜像源的恶意游标）
   直接报错不请求，防止流量外泄（tags_test.go:464-488）。**已于第二轮安全审查加固**：
   校验从"只比 Host"收紧为"scheme + host + userinfo 三者必须与 base 完全一致"——
   异 scheme（http 游标 vs https base，防降级明文）与注入 userinfo
   （`https://evil@base-host/...`）的游标均被拒绝；错误消息里的 URL 走 redactURL 脱敏。
4. **分页环保护**：`visited` 集合（tags.go:35,39-41），next 指回已请求过的 URL 即报
   `pagination loop detected`，`--all` 遇脏数据源不再挂死（tags_test.go:538-561）。
   **已于第二轮安全审查加固**：环检测只防"精确重复 URL"，镜像源每页返回唯一新游标
   仍会无限翻页、visited 无界增长——新增 `maxPages = 1000` 页数硬上限（tags.go:20,
   44-46），超限报 `tags pagination exceeded %d pages`。
5. **`user`/`image` PathEscape**（tags.go:33），见第 2 节。
6. **错误消息带 URL**：search.go:29、tags.go:55-57 的非 2xx 错误含完整 URL，配合 `DHS_API_BASE`
   镜像源可定位出错 base（search_test.go:176-188、tags_test.go:518-530）。
   **已于第二轮安全审查加固**：所有打印 URL 的错误统一经 `redactURL`（tags.go:134-142）
   脱敏——URL 含 userinfo（如 `https://user:pass@mirror`）时密码被 `xxxxx` 掩码，
   不再随错误输出泄漏（tags_test.go / search_test.go 各有 RedactsUserinfoPassword 钉测试）。

### resolveNext 行为矩阵（tags.go:104-121）

| next 形态 | 行为 | 测试 |
| --- | --- | --- |
| 相对路径（无 scheme，如 `/v2/...?page=2`） | `baseURL.ResolveReference(nextURL)` 解析为绝对 URL，scheme/host/userinfo 继承 base，校验通过后请求 | tags_test.go:433-459 |
| 绝对 URL、与 base 同 scheme 同 host 且无 userinfo | 原样使用 | tags_test.go:84-117（多页翻页） |
| 绝对 URL、异 host | 拒绝：`deviates from the configured base`，不发起请求 | tags_test.go:464-488 |
| 协议相对 URL（`//evil.example.com/...`） | ResolveReference 后 host 与 base 不符 → 拒绝（修复前 `Scheme==""` 分支穿透放行） | tags_test.go `TestFetchTags_ProtocolRelativeNextCursorMustBeRejected` |
| 绝对 URL、同 host 异 scheme（http 游标 vs https base） | 拒绝：scheme 必须与 base 一致（防降级明文） | tags_test.go `TestFetchTags_NextCursorSchemeMismatchMustBeRejected` |
| 带 userinfo 的游标（`https://evil@base-host/...`） | 拒绝：游标不得携带与 base 不同的 userinfo | tags_test.go `TestFetchTags_NextCursorWithUserinfoMustBeRejected` |
| 指向本次抓取已访问过的 URL（精确重复环） | 循环顶部 `visited[u]` 命中即报错，不发请求 | tags_test.go:538-561 |
| 每页唯一新游标（非精确重复环） | 翻满 `maxPages`（默认 1000，测试可改小）即报 `tags pagination exceeded %d pages` | tags_test.go `TestFetchTags_PaginationExceedsMaxPages` |
| URL 解析失败（如含控制字符的畸形游标） | `parsing tags next cursor` 错误 | tags_test.go `TestFetchTags_MalformedNextCursorRejected`；单元级矩阵 `TestResolveNext_CursorMustMatchBaseSchemeHostUserinfo` |

## 5. 重构方案总览

| 编号 | 优先级 | 主题 | 工作量 |
| --- | --- | --- | --- |
| R1 | **P1（未修）** | 代理校验双份且规则不一 | S |
| R2 | P2 | GET→解析骨架重复 + ReadAll 无上限 | M |
| R3 | P2 | tagsPageSleep 500ms 写死 | S |
| R4 | P2 | 429/401/403 无专属提示 | S |
| R5 | P3 | 死字段三个 | XS |
| R6 | P3 | archSet 空集语义未文档化 | XS |
| R7 | P3 | SizeMB 实为 MiB | S（跨包） |
| R8 | P3 | client_test.go 注释与实现不符——**已于本轮修复**（含全包"预期红"死注释清扫，见 R8 明细） | XS |

## 6. 重构项明细

### R1（P1，未修）：NewClient 代理校验与 cmd/config.go 双份且规则不一

现状：同一字符串在两个入口得到不同结论。`dockerhub.NewClient`（client.go:24-31）只要求
`url.Parse` 成功且 scheme 非空——任意 scheme（`ftp://x`）和无 host 的 `http://` 都接受；
`cmd.applySetting`（cmd/config.go:36-39）则要求 scheme **和** host 都非空，同样没有 scheme 白名单。
后果：`ftp://h:1` 能写进配置文件；`http://`（无 host）在 config set 被拒、在 `--proxy`
路径（走 NewClient）却被接受——同一个值两处判定相反。socks5 两边都意外可用，但从未被显式认可。

方案：在 `internal/config` 增加 `ValidateProxy`（cmd 与 dockerhub 都已依赖 config，不引入环）：

```go
// internal/config/proxy.go
var proxySchemes = map[string]bool{"http": true, "https": true, "socks5": true}

// ValidateProxy 校验代理地址：scheme ∈ {http,https,socks5} 且 host 非空。
func ValidateProxy(raw string) error {
    u, err := url.Parse(raw)
    if err != nil {
        return fmt.Errorf("invalid proxy URL %q: %w", raw, err)
    }
    if !proxySchemes[u.Scheme] || u.Host == "" {
        return fmt.Errorf("invalid proxy URL %q: want http/https/socks5 with host", raw)
    }
    return nil
}
```

`cmd/config.go:36-39` 与 `client.go:24-31` 都改为调用它，用例扩充 ftp://、socks5://、无 host。
工作量：S——注意 `e2e/e2e_test.go:170-176` 钉了 `no-scheme` 的中文报错文案，替换时保留或同步。

### R2（P2）：search.go 与 tags.go 的 GET→解析骨架重复；io.ReadAll 无大小上限

现状：search.go:22-40 与 tags.go:47-67 是同一段"GET → 状态码 → ReadAll → Unmarshal"的复制，
两处 `io.ReadAll(resp.Body)`（search.go:32、tags.go:59）都无上限——恶意/故障的镜像源（配合
`DHS_API_BASE`）可以无限流喂大内存。search_test.go:205-237 已验证 1MB 大 body 可解析，
但"超限应报错"的测试尚缺。

方案：抽一个包内 helper，统一 LimitReader 上限（10MB 足够：一页 tags 100 条远小于此）：

```go
const maxBodyBytes = 10 << 20 // 10MB

// fetchJSON GETs u, enforces maxBodyBytes, and decodes into dst.
func fetchJSON(client *http.Client, u string, dst any) error {
    resp, err := client.Get(u)
    if err != nil {
        return err // 调用方 wrap 上 "search/tags request failed" 前缀
    }
    defer resp.Body.Close()
    if resp.StatusCode == http.StatusNotFound {
        return errNotFound // tags 侧 wrap 成 "image %s/%s not found"
    }
    if resp.StatusCode < 200 || resp.StatusCode >= 300 {
        return fmt.Errorf("docker hub returned HTTP %d for %s", resp.StatusCode, u)
    }
    body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes+1))
    if err != nil {
        return fmt.Errorf("reading response: %w", err)
    }
    if len(body) > maxBodyBytes {
        return fmt.Errorf("response from %s exceeds %dMB limit", u, maxBodyBytes>>20)
    }
    return json.Unmarshal(body, dst) // 调用方 wrap "parsing ... response"
}
```

两个 Fetch 改为构造 URL + 循环里调 `fetchJSON`，保留各自错误前缀（cmd/e2e_test.go 有文案断言）。
工作量：M——重构 + 补"11MB body 报错"钉测试；现有钉测试（search_test.go:143-168、tags_test.go:352-371）
应全部保持绿色。

### R3（P2）：tagsPageSleep 500ms 写死，--all 大仓库慢

现状：tags.go:14 定义、tags.go:90 每翻一页睡 500ms（沿用 ps1 的礼貌限速）。5000 tag 的仓库
约 50 页，纯 sleep 就 25 秒，`--all` 体验差。

方案（两档，可叠加）：直接降为 100-200ms（Docker Hub 对匿名 tags 端点的实际限频远宽于 2 req/s）；
或按响应自适应——429 时读 `Retry-After` 退避重试，正常页间只短睡：

```go
if resp.StatusCode == http.StatusTooManyRequests {
    time.Sleep(retryAfter(resp, time.Second)) // 上限 30s
    continue // 同一 u 重试；重试不算环，必须在 visited[u]=true 之前 continue
}
time.Sleep(100 * time.Millisecond)
```

注意环检测（tags.go:39-41）以 URL 为键，重试同一 URL 会误判成环——重试分支要放在
`visited[u] = true` 之前或用独立的 retryBudget。工作量：S。

### R4（P2）：429/401/403 无专属提示

现状：tags.go:55-58、search.go:28-30 对所有非 2xx 一律 `HTTP %d for %s`，用户分不清
"限流，稍后再试"与"镜像源配错/需要认证"（cmd/e2e_test.go:396-407 只断言 HTTP 429）。

方案：在 fetchJSON（R2）里加状态到提示的映射：

```go
func statusHint(code int) string {
    switch code {
    case http.StatusTooManyRequests:
        return "（请求过于频繁，请稍后重试或降低 --limit）"
    case http.StatusUnauthorized, http.StatusForbidden:
        return "（镜像源拒绝访问，请检查 DHS_API_BASE 或代理配置）"
    }
    return ""
}
```

工作量：S（含 2-3 个文案钉测试）。

### R5（P3）：死字段 Repo.LastUpdated / paginatedTags.Previous / paginatedRepos.Next

- `Repo.LastUpdated`（types.go:65）：search API 不返回 last_updated（types.go:28-29 注释），
  search.go:44-50 构造 Repo 时不赋值，永远空串；cmd 层无人读取。
- `paginatedTags.Previous`（types.go:23）与 `paginatedRepos.Next`（types.go:41）：只解析从不读取，
  search 单页设计下后者永远用不到。

方案：直接删除（grep 确认无引用，JSON 反序列化容忍缺字段）。保留 `Next` 的唯一理由是
"未来 search 也翻页"，届时再加回并真正使用。工作量：XS。

### R6（P3）：archSet 空集=不匹配的语义未文档化

现状：`windows/amd64`-only 的 tag 在默认视图下 `archSet` 为空，`HasArch` 恒 false
（tags_test.go:306-309、387-392 钉住），被任何 `--arch` 过滤掉——这是刻意行为（过滤语义就是
"linux 架构可用性"），但 types.go:54-55 的注释没写，维护者只能翻测试才知道。

方案：补 godoc（types.go:54-55 与 tags.go:186-188），例如：`An EMPTY set (e.g. a windows-only
tag under the default view) matches no --arch target: HasArch always returns false.` 工作量：XS，纯注释。

### R7（P3）：SizeMB 实为 MiB

现状：tags.go:73 `float64(tr.FullSize) / 1024.0 / 1024.0` 算的是 MiB，但字段名 `SizeMB`（types.go:51）、
表头 `Size(MB)`（internal/ui/table.go:101）与导出 markdown 表头（cmd/tags.go:258）都写 MB。
数值自洽（tags_test.go:246-260 用 50×1024×1024 验证 50.0），只是单位命名不严谨。

方案二选一：字段改名 `SizeMiB` 并同步表头（跨 3 个包 + 导出文件格式，用户可见变化）；或保持 MB
惯例（容器生态普遍混用，Docker Hub 自家 UI 也写 MB），仅在 types.go:51 注释"实际按 1024 进制换算"。
建议后者，工作量：XS（注释）或 S（改名）。

### R8（P3，**已于本轮修复**）：client_test.go 一处注释与实际分支不符

现状（修复前）：client_test.go:82-85 的注释仍写"DHS_API_BASE 带尾斜杠时 BaseURL() 原样返回
（不做 TrimSuffix）……=> 红"。但 client.go:49 已实现 TrimSuffix，该测试现在是绿的——注释描述的
失败分支已不存在。

已于本轮修复（2026-10 第二轮审查）：client_test.go:82-85 的注释改为【绿·钉修复】语义，钉住
"尾斜杠必须被 Trim"这一契约；同时清扫了全包其余过时的【预期红·钉 bug】注释——tags_test.go
（相对游标/越界拒绝/PathEscape/错误带 URL 四处）、search_test.go（错误带 URL）——这些测试
均已转绿，注释统一改为【绿·钉修复】+ 过去时描述。现有测试名均为契约式命名
（Should/Must 风格，grep 无 Fixed* 残留），无需改名。

## 7. 维护注意事项

- 本包测试大量使用"钉测试"（【预期红】/【绿·钉现状】/【绿·钉修复】注释）驱动修复：改行为前先读对应测试注释块，红转绿或行为变更时同步更新注释与本文档第 4 节。
- `tagsPageSleep` 与 `maxPages` 是包级可变状态，同包测试不可 `t.Parallel()`（见 e2e-and-ci.md 第 1 节）；测试中改小后必须经 `t.Cleanup` 恢复。
- 新增端点遵循同一模式：非导出原始结构（types.go）+ 导出归一化结构 + Fetch 函数收 `(client, base, ...)`，错误消息一律带 URL，且 URL 一律经 `redactURL` 脱敏后再打印（userinfo 密码不落终端）。
