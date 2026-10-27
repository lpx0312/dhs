# internal/ui — 模块文档与重构方案

> 范围：`internal/ui/{color,table}.go` 及配套测试。
> 定位：纯函数渲染包——输入预格式化的字符串，输出最终展示文本；零 I/O、零网络、零配置读取。

## 1. 职责与边界

- 两个文件：`color.go` 负责颜色包装，`table.go` 负责表格对齐与 docker pull 命令生成。
- 所有导出函数都是 `string → string` 的纯函数，不碰 `os.Stdout`、环境变量或时间——输出时机与
  目的地完全由 cmd 层决定（`fmt.Print(ui.RenderRepoTable(rows))`，cmd/search.go:57）。
- **行结构存预格式化字符串**：`RepoRow`/`TagRow`（table.go:23-38）的字段全是 string，
  数字格式化（`strconv.FormatFloat(..., 'f', 1, 64)`，cmd/tags.go:224）、日期截断
  （`shortDate`，cmd/root.go:74-79）、✓/✗ 上色（cmd/search.go:44-47）都在 cmd 完成。
  这个取舍的讨论见第 4 节 R6。

## 2. color.go — 颜色语义

- 包内 5 个 `*color.Color` 全局变量（cyan/green/yellow/red/bold，color.go:7-21），
  导出 `Cyan/Green/Yellow/Red/Bold` 一律走 `Sprint`（color.go:37-41）。
- **`SetEnabled` 只禁不开**（color.go:30-34，本轮修复）：`enabled=false` 时置
  `color.NoColor = true`；`enabled=true` 时**什么都不做**。fatih/color 在包 init 时已按
  no-color.org 约定（非空 `NO_COLOR`）与非 TTY stdout 自动判定 NoColor，若 SetEnabled(true)
  强制写 `false` 会同时破坏这两个检测——修复前正是这个 bug（color_test.go:79-97 钉住
  `NO_COLOR=1 + SetEnabled(true)` 必须无 ANSI；cmd/e2e_test.go:931-945 的进程内 e2e 契约同源）。
- cmd 侧入口是 `applyColorSetting`（cmd/root.go:59-63）：只在 `EffectiveNoColor` 为真时调
  `SetEnabled(false)`，从不强制开色。

## 3. table.go — 表格与 pull 命令

- **stripANSI**（table.go:11-18）：正则剥离 SGR 转义序列。`renderTable` 的**宽度计算**
  用剥离后的长度（table.go:51、55、64，本轮修复），**输出**仍原样写带色字符串——同一个
  `ui.Red("✗")` 单元格在彩色/裸两种模式下列起始偏移、分隔线宽度一致
  （table_test.go:131-151 钉住该契约）。
- **renderTable**（table.go:48-87）：列宽 = max(表头宽度, 各行宽度)，列距 3 空格，表头下
  分隔线为各列宽度的 `-`。末列也补齐，行尾留悬挂空格（table_test.go:175-181 钉住现状）。
- **FormatPullCommands**（table.go:114-130）：
  - `user=="library"` 时镜像名省略 `library/` 前缀（table.go:116-118，对齐 ps1 行为）；
  - 排序用 `sort.SliceStable`（table.go:119，本轮修复）：重复 tag 保持输入顺序，修复了
    `sort.Slice`（pdqsort 非稳定）下同名 tag 内 amd64/arm64 命令错位的问题
    （table_test.go:253-294 钉住）；注意它**原地重排调用方切片**（table_test.go:343-352 钉住）；
  - 命令生成只认两种架构：amd64 → 裸 `<image>:<tag>`；arm64 → `--platform=linux/arm64 <image>:<tag>`
    （table.go:122-127）；其他架构（386、arm、riscv64…）**静默产出 0 条**
    （table_test.go:229-238），见 R2。

## 4. 重构方案

### R1（P2）：CJK/emoji 字节宽度错位

现状：宽度计算是 `len(stripANSI(...))`（table.go:51、55、64）——仍是**字节长度**。CJK 一个汉字
3 字节但占 2 列，右列会向左错位；emoji（4 字节、2 列）同理。测试只钉了"不 panic、内容在"
（table_test.go:110-119），承认错位。镜像描述（`short_description`）是中文重灾区。

方案：引入显示宽度函数替换 `len`。优先 `github.com/mattn/go-runewidth`（fatih/color 的
同系作者，纯 Go、无传递依赖）；不愿加依赖则手写简表：

```go
// displayWidth 返回可见列宽：ASCII 计 1，East Asian Wide/Fullwidth 计 2，
// 组合附加符计 0。覆盖常用区间即可，emoji ZWJ 序列允许近似。
func displayWidth(s string) int {
    w := 0
    for _, r := range s {
        w += runeWidth(r) // runeWidth: 查 Wide/Fullwidth 区间表
    }
    return w
}
```

`renderTable` 三处 `len(stripANSI(x))` 改为 `displayWidth(stripANSI(x))`；补一个
"中文单元格后右列对齐"的钉测试（现 CJK 测试改为断言对齐）。工作量：M（含区间表）或 S（用库）。

### R2（P2）：FormatPullCommands 只支持 amd64/arm64，其他 --arch 静默 0 条

现状：`--arch 386 --gen-pull` 能过滤出 tag，但命令数为 0（cmd/e2e_test.go:646-659 钉住
"共生成 0 条命令"），用户没有任何提示——386/arm 树莓派等场景完全不可用。

方案：泛化为对**请求的每个架构**生成命令，统一 `--platform=linux/<arch>` 形式：

```go
for _, arch := range archOrder(s.Arches) { // amd64 最先，其余按 Arches 迭代序
    if arch == "amd64" {
        out = append(out, fmt.Sprintf("%s:%s", imageName, s.Tag))
    } else {
        out = append(out, fmt.Sprintf("--platform=linux/%s %s:%s", arch, imageName, s.Tag))
    }
}
```

兼容性取舍：amd64 省略 `--platform` 是 ps1 时代的历史约定（省字符），泛化后可选择保留例外
（推荐，输出 diff 最小）或全部带 `--platform`（更一致）。现有钉测试
（table_test.go:219-227、229-238）需按新语义更新。工作量：S。

### R3（P3）：writeRow 越界 panic 隐患

现状：`writeRow` 遍历整行单元格，而 `widths` 只有 `len(headers)` 项（table.go:61-73）；
数据行列数超过表头时 `widths[i]` 直接 index out of range panic（table_test.go:187-194 钉住
现状）。当前两个调用方列数固定一致，属于防御性缺口而非活跃 bug。

方案：循环上界改为 `min(len(cells), len(widths))`（多出的单元格照写不补齐），或 renderTable
入口按行最大列数扩宽 widths。同时把钉测试从"断言 panic"翻转为"断言不 panic"。工作量：XS。

### R4（P3）：Bold 死代码

现状：`Bold`（color.go:41）在 cmd/ 与 main 中无任何生产调用（grep 仅 color.go 定义与
color_test.go:47 测试引用）。方案：连同 `bold` 变量（color.go:12、20）与测试引用一并删除。
工作量：XS。

### R5（P3）：Sprint 绕过 go-colorable 的 Windows 旧终端兼容

现状：fatih/color 的 `Print*` 系列在 Windows 上经 `go-colorable` 把 ANSI 转成 Console API
调用，兼容不启用 VT 的旧 conhost；本项目所有输出都是 `fmt.Print(ui.Cyan(...))` 组合——
`Sprint` 只产 ANSI 字符串，绕过了这层转换。go-colorable 在 go.mod 中仅作为间接依赖存在，
从未被直接使用。Windows 10 1511+ 默认启用 VT，实际风险低，但 Win7/8 或特殊终端下会显示
乱码转义序列。

方案：接受现状（目标用户 Win10+）并在本文档记录；或 cmd 层统一经
`colorable.NewColorableStdout()` 写出（改动面大，需替换所有 fmt.Print*）。建议前者。工作量：XS（记录）。

### R6（设计权衡）：行结构存预格式化 string vs 语义值

现状是前者：`RepoRow.Description` 已被 cmd 截断到 40 rune（cmd/search.go:53、69-75），
`TagRow.Size` 已是 `%.1f` 字符串。得失：

- **预格式化（现状）**：ui 保持零依赖、零决策，纯排版机；代价是 ui 无法做语义级操作——
  按数值排序、按剩余宽度动态截断、右对齐数字列都得回 cmd 做；宽度计算（R1）也只能对
  最终字符串做。
- **语义值（如 `Stars int64`）**：ui 可承担对齐格式（`%10d`）、动态截断、未来加"按 stars
  排序后重渲染"；代价是 ui 要理解 strconv/时间格式，cmd/ui 责任边界变糊，且现有全部
  钉测试的输入构造方式要重写。

结论：维持现状。仅当 R1 落地后仍需动态列宽/截断（例如超窄终端）时，再考虑把
`Description` 一类"自由文本列"单独改为语义值 + ui 内截断的混合方案。

## 5. 优先级汇总

| 编号 | 优先级 | 主题 | 工作量 |
| --- | --- | --- | --- |
| R1 | P2 | CJK/emoji 字节宽度错位 | S-M |
| R2 | P2 | --arch 非 amd64/arm64 时 gen-pull 静默 0 条 | S |
| R3 | P3 | writeRow 越界 panic 隐患 | XS |
| R4 | P3 | Bold 死代码 | XS |
| R5 | P3 | Sprint 绕过 go-colorable | XS（记录） |
| R6 | — | 行结构 string vs 语义值（权衡，倾向维持现状） | — |
