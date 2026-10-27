# internal/config 模块文档与重构方案

> 对象：`internal/config/config.go`（约 100 行）及其测试。被 `cmd` 包单向依赖；不依赖其他 internal 包。

## 1. 职责与公开 API

| 成员 | 位置 | 职责 |
| --- | --- | --- |
| `Config` | config.go:14-17 | 仅两个字段：`Proxy string`（yaml `proxy`）、`NoColor bool`（yaml `no_color`）。flag 值不走结构体，显式传参给 Effective* 以保持优先级可测 |
| `Load(path)` | config.go:21-37 | 读 YAML；文件不存在返回零值 Config 不报错（:28-30），path 为空同样零值（:23-25）；坏 YAML/类型不匹配原样返回 yaml 错误（:33-35） |
| `Save(path, cfg)` | config.go:76-90 | `MkdirAll(dir, 0700)` → `yaml.Marshal` → `os.WriteFile(path, data, 0600)`（:77-88） |
| `DefaultConfigPath()` | config.go:94-100 | `os.UserConfigDir()` + `/dhs/config.yaml` |
| `EffectiveProxy` | config.go:41-55 | 优先级链见下 |
| `EffectiveNoColor` | config.go:61-72 | 优先级链见下 |

**代理优先级链**（config.go:41-55）：`--proxy` flag > 配置文件 `proxy` > `HTTPS_PROXY` > `HTTP_PROXY` > 直连("")。只查大写变量（:48-53）。判定即"第一个非空者胜"，因此文件里存了仅空白的 proxy 也会压过环境变量并被原样返回（已被 config_test.go:430-437 钉住，配套的"Load 不 TrimSpace"钉在 :400-425）。

**颜色优先级链**（config.go:61-72）：`--no-color` flag > 配置文件 `no_color` > `NO_COLOR` env（**本轮新增层**，:68-70）。遵循 no-color.org 约定：任意非空值即禁色，空值忽略——空值语义已被 config_test.go:301-311 钉住。flag 与文件都未设置时返回 false，fatih/color 自身的 TTY/NO_COLOR 检测不被打扰（与 `cmd.applyColorSetting` 的"只禁不开"配套，root.go:70-74、ui/color.go:30-34）。

两条链的职责边界：本包只做**取值裁决**（返回该用哪个代理/是否禁色）；副作用（改 color 全局开关、构造 Transport）分别归 `cmd`/`internal/ui` 与 `internal/dockerhub`。

## 2. 配置文件格式与位置

- 格式：极简 YAML，两键。`dhs config set` 写出的形如：

```yaml
proxy: http://192.168.0.225:7897
no_color: false
```

  `yaml.Marshal` 全量重写，注释与未知字段不保留（见 §4-R3）；`Load` 对未知字段静默忽略以保证前向兼容（config.go:33 反序列化进结构体，钉在 config_test.go:442-458）。
- 位置（`os.UserConfigDir()`，config.go:94-100）：

| 平台 | 路径 |
| --- | --- |
| Windows | `%AppData%\dhs\config.yaml` |
| Linux | `$XDG_CONFIG_HOME/dhs/config.yaml`（未设则 `~/.config/dhs/config.yaml`） |
| macOS | `~/Library/Application Support/dhs/config.yaml` |

  `--config` flag 可整体覆盖该路径（cmd/root.go:51-56）。三个平台变量都缺失时 `DefaultConfigPath` 报错（钉在 config_test.go:525-541）。
- **0600 权限说明**：Save 声明 0600（config.go:75-76 注释：代理 URL 可能内嵌凭据），目录 0700（:78）。两个事实：(a) `os.WriteFile` 权限位仅创建时生效，覆盖已有 0644 文件不会收紧——已被 config_test.go:355-374 钉住，修复方向是写后 `os.Chmod(path, 0600)`（§4-R1 的骨架顺带解决）；(b) **Windows 上这些 POSIX 位无 ACL 效果**，Go 仅用 mode 的只读位，权限保护实际不存在（两个权限测试在 windows 跳过，config_test.go:356-358/380-382）。

## 3. 近期变更（本轮已修复）

- `EffectiveNoColor` 补齐 `NO_COLOR` env 层，最终层级 flag > file > env（config.go:61-72）。8 组合优先级表 + env 空值忽略均有绿钉：config_test.go:256-311。
- 配套的 `cmd.applyColorSetting`/`ui.SetEnabled` 改为"只禁不开"，详见 docs/modules/cmd.md §7。
- 代理 env 回退测试（`TestEffectiveProxy_FallsBackToEnv` / `TestEffectiveProxy_EnvHTTPSFallback`）由 `os.Setenv` + 手工 defer 恢复迁至 `t.Setenv`（自动恢复原值，并自动禁并行，原写法不恢复原值/漏恢复 HTTPS_PROXY 的隐患消除）：config_test.go:68-90。
- cmd 层本轮另修复 rootCmd `SilenceUsage` 与 `errWithProxy` 凭据脱敏（均不涉及本包，见 docs/modules/cmd.md §7）。

## 4. 重构方案

### R1（P1）Save 非原子

- **现状**：config.go:86 直接 `os.WriteFile` 截断写。进程在写中途被杀会留下半个 YAML；并发 `Load` 可能读到截断/空文件——空文件恰好被"缺文件=零值"逻辑吞成零配置且无报错（config.go:28-30 对存在但空的文件同样零值，config_test.go:126-136），故障静默。现状被 config_test.go:314-348 以可观察行为（O_TRUNC 无残留字节）钉住，原子性本身未修。
- **方案**：同目录 temp + rename：
```go
func Save(path string, cfg *Config) error {
    // ...MkdirAll/Marshal 不变...
    tmp, err := os.CreateTemp(filepath.Dir(path), ".config-*.tmp")
    if err != nil { return fmt.Errorf("creating temp config: %w", err) }
    defer os.Remove(tmp.Name())            // rename 成功后是 no-op
    if _, err := tmp.Write(data); err != nil { tmp.Close(); return ... }
    if err := tmp.Chmod(0600); err != nil { ... }   // 顺带修 R-权限：每次写都收紧
    if err := tmp.Close(); err != nil { ... }
    if err := os.Rename(tmp.Name(), path); err != nil { return fmt.Errorf("replacing config: %w", err) }
    return nil
}
```
- **Windows 说明**：`os.Rename` 目标已存在且为普通文件时会替换（Go 用 MoveFileEx + MOVEFILE_REPLACE_EXISTING），可正常覆盖；但官方文档明确 Windows 上 rename **不是原子操作**（POSIX 同目录 rename 原子）。即：Windows 得到"不会留半个文件"的保证（写入发生在 tmp），得不到严格的原子替换窗口；对单机 CLI 足够，文档注明即可。
- **工作量**：0.5 天（含权限钉住测试 config_test.go:355-374 反转为"始终 0600"）。

### R2（P2）并发 `config set` 丢更新

- **现状**：cmd/config.go:95-104 的 set 流程是 Load→改内存→Save 的读-改-写，无任何锁；两个并发 set 各自基于旧快照，后写者覆盖前写者（last-writer-wins，且是**键级**丢更新：A 进程 set proxy、B 进程同时 set no-color，先完成者的写入整体消失）。unset 同理（cmd/config.go:185-199）。
- **方案**：两档任选：①**文档化**——在 configCmd.Long 声明"并发写不保证合并，最后写入生效"（成本 10 分钟，配合 R1 原子写后不产生损坏文件）；②锁文件——`O_CREATE|O_EXCL` 创建 `<path>.lock` + 重试超时，POSIX/Windows 均可移植（flock/LockFileEx 不可移植，不采用）：

```go
lock := path + ".lock"
for i := 0; i < 50; i++ {           // ~5s 退避重试
    f, err := os.OpenFile(lock, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
    if err == nil {
        t.Cleanup(func() { os.Remove(lock) }) // 概念示意；生产放 defer + 写 PID
        break
    }
    time.Sleep(100 * time.Millisecond)
}
// ...Load → mutate → Save...
```

  死锁残留（进程崩溃未删 lock）需按 mtime 过期回收。推荐 ①，等出现真实并发场景再上 ②。**工作量**：①0.5 小时 / ②1 天。

### R3（P2）Load→Save 丢注释/未知字段

- **现状**：Load 反序列化进结构体（config.go:33），Save 用 `yaml.Marshal` 全量重写（config.go:82），用户手写注释和未知字段在第一次 `config set` 后消失。前向兼容的"未知字段忽略"已被 config_test.go:442-458 钉住。
- **方案**：低成本：在 README/`configCmd.Long` 声明"该文件由 dhs 托管，勿手写注释"；高成本：`yaml.Node` 往返保留注释与未知键（node.Decode/Encode 按 key 定位修改）。当前仅两键，托管声明足够。**工作量**：0.5 小时（文档）。

### R4（P2）EffectiveProxy 弃用形参 `_ string`

- **现状**：签名 `EffectiveProxy(flagProxy string, _ string) string`（config.go:41），第二形参无任何用途；调用点共 10 处：生产 3 处（cmd/search.go:30、cmd/tags.go:47、cmd/config.go:169）+ 测试 7 处（internal/config/config_test.go 中 `.EffectiveProxy(` 共 7 次调用）。无测试钉住签名。
- **方案**：删形参，`grep -l '\.EffectiveProxy(' --include='*.go' | xargs sed -i 's/EffectiveProxy(flagProxy, "")/EffectiveProxy(flagProxy)/'` 式的机械替换 + `go build ./...` 验证。**工作量**：0.5 小时。

### R5（P2）小写 https_proxy 只在 Windows"偶发生效"

- **现状**：只查大写 `HTTPS_PROXY`/`HTTP_PROXY`（config.go:48-53）。三平台事实不一致：Windows 环境变量名大小写不敏感，小写 `https_proxy` 被 `os.Getenv("HTTPS_PROXY")` 命中，EffectiveProxy 返回它（"碰巧生效"，config_test.go:466-480 的 windows 分支钉住）；Linux/macOS 上 EffectiveProxy 返回空 → `dockerhub.NewClient("")` 走 `ProxyFromEnvironment`（client.go:33-34），而标准库**会**读小写——结果是请求走了小写代理，但 `config list` 显示"直连"、`errWithProxy` 不标注，展示与行为分裂（POSIX 分支钉住在 config_test.go:477-479）。
- **方案**：补读小写（与 net/http 一致的大写优先、小写兜底）：
```go
for _, k := range []string{"HTTPS_PROXY", "https_proxy"} {
    if v := os.Getenv(k); v != "" { return v }
}
// HTTP_PROXY/http_proxy 同理
```
或文档化"仅支持大写变量"。推荐补读，消除三平台不一致。**工作量**：1 小时（含钉住测试翻转：posix 分支期望从 "" 变为小写值）。

### R6（P2）显式代理时 NO_PROXY 失效

- **现状**：`--proxy`/配置文件给出代理后，`NewClient` 用 `http.ProxyURL(u)` 固定走该代理（client.go:24-32），环境变量路径 `ProxyFromEnvironment`（含 NO_PROXY 排除逻辑）被整体绕过；且 EffectiveProxy 返回非空时连回退判断都没有——用户设了 `NO_PROXY=hub.docker.com` 也无法豁免。
- **方案**：文档化（Long/help 注明"显式代理忽略 NO_PROXY"），或组合 `golang.org/x/net/http/httpproxy`：
```go
// client.go 内
cfg := httpproxy.Config{Proxy: proxyURL} // 留空 NoProxy 时仍无排除；再读 NO_PROXY 填充
transport.Proxy = cfg.ProxyFunc()
```
推荐先文档化（行为已长期如此，用户无投诉路径），需求出现再引依赖。**工作量**：文档 0.5 小时 / httpproxy 0.5 天。

### R7（P3）坏 YAML 错误不带路径

- **现状**：config.go:33-35 把 yaml 错误原样上抛，不含文件路径；用户配了 `--config` 多份配置时难以定位。已被 config_test.go:486-499 反向钉住（断言错误**不含**路径）。
- **方案**：`return nil, fmt.Errorf("parsing config %s: %w", path, err)`，翻转钉住测试。**工作量**：15 分钟。

### 钉住状态速查

| 项 | 钉住位置 | 修复时需翻转 |
| --- | --- | --- |
| R1 非原子 Save | config_test.go:314-348（行为级） | 权限测试 :355-374 |
| R2 并发丢更新 | 无 | — |
| R3 丢注释/未知字段 | config_test.go:442-458（钉"忽略"） | 否（声明即可） |
| R4 弃用形参 | 无 | — |
| R5 小写 env | config_test.go:466-480（双平台分支） | 是（posix 分支） |
| R6 NO_PROXY 失效 | 无 | — |
| R7 错误不带路径 | config_test.go:486-499 | 是 |
