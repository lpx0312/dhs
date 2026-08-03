# 发布指南

本文档涵盖两种发布方式：**手动发布**（tea CLI）与 **自动流水线**（Gitea Actions）。

---

## 一、手动发布（tea CLI）

适用于快速发布，直接在本地编译、打 tag、上传。

### 1.1 编译多平台二进制

```powershell
# Windows PowerShell
$env:CGO_ENABLED="0"

$env:GOOS="linux";   $env:GOARCH="amd64";  go build -ldflags="-s -w" -o build/dhs-linux-amd64    .
$env:GOOS="linux";   $env:GOARCH="arm64";  go build -ldflags="-s -w" -o build/dhs-linux-arm64    .
$env:GOOS="windows"; $env:GOARCH="amd64";  go build -ldflags="-s -w" -o build/dhs-windows-amd64.exe .
$env:GOOS="darwin";  $env:GOARCH="amd64";  go build -ldflags="-s -w" -o build/dhs-darwin-amd64   .
$env:GOOS="darwin";  $env:GOARCH="arm64";  go build -ldflags="-s -w" -o build/dhs-darwin-arm64   .
```

| 文件 | 平台 | 说明 |
|------|------|------|
| `dhs-windows-amd64.exe` | Windows 64 位 | 主流 Windows |
| `dhs-linux-amd64` | Linux x86_64 | 服务器/桌面 |
| `dhs-linux-arm64` | Linux ARM64 | 树莓派/ARM 服务器 |
| `dhs-darwin-amd64` | macOS Intel | Intel Mac |
| `dhs-darwin-arm64` | macOS Apple Silicon | M1/M2/M3 Mac |

> `-ldflags="-s -w"` 去除调试符号，缩小二进制体积约 30%。

### 1.2 打 Tag 并推送

```bash
git tag v0.2.0 -m "v0.2.0 - 新增 XXX 功能"
git push origin v0.2.0
```

### 1.3 创建 Release 并上传二进制

```bash
tea releases create --repo study/dhs \
  --tag v0.2.0 \
  --title "v0.2.0" \
  --note "## 更新内容`n`n- 新增功能 A`n- 修复问题 B" \
  --asset build/dhs-windows-amd64.exe \
  --asset build/dhs-linux-amd64 \
  --asset build/dhs-linux-arm64 \
  --asset build/dhs-darwin-amd64 \
  --asset build/dhs-darwin-arm64
```

### 1.4 tea 常用 release 命令

| 命令 | 说明 |
|------|------|
| `tea releases list --repo study/dhs` | 查看所有 release |
| `tea releases create --repo study/dhs --tag v0.2.0 ...` | 创建 release |
| `tea releases edit --repo study/dhs --tag v0.2.0 --title "新标题"` | 编辑 release |
| `tea releases delete --repo study/dhs --tag v0.2.0` | 删除 release |

---

## 二、自动流水线（Gitea Actions）

一次配置，以后每次推送 tag 自动编译所有平台并上传 Release。

### 2.1 工作原理

```
git tag v0.2.0 → git push → Gitea 检测到 v* 标签
                                  ↓
                          Runner 拉取代码
                                  ↓
              ┌───── Go 交叉编译 5 个平台 ─────┐
              │ linux/amd64    linux/arm64      │
              │ windows/amd64  darwin/amd64     │
              │ darwin/arm64                    │
              └────────────────────────────────┘
                                  ↓
                   创建 Release + 上传所有二进制
```

### 2.2 部署 Runner（在 192.168.0.180 上执行一次）

#### 步骤 1：下载 act_runner

```bash
wget https://dl.gitea.com/act_runner/v0.2.11/act_runner-0.2.11-linux-amd64
chmod +x act_runner-0.2.11-linux-amd64
```

#### 步骤 2：获取注册 Token

浏览器打开：`http://192.168.0.180:9000/admin/actions/runners`

点击 **创建新的 Runner**，复制显示的 token。

#### 步骤 3：注册 Runner

```bash
./act_runner-0.2.11-linux-amd64 register \
  --instance http://192.168.0.180:9000 \
  --token <粘贴你的token> \
  --name dhs-runner \
  --no-interactive
```

#### 步骤 4：启动 Runner

```bash
# 后台运行
nohup ./act_runner-0.2.11-linux-amd64 daemon > runner.log 2>&1 &

# 查看日志
tail -f runner.log
```

#### 步骤 5：启用仓库 Actions

打开 `http://192.168.0.180:9000/study/dhs/settings`

勾选 **Enable Repository Actions**。

### 2.3 流水线配置文件

项目中的 `.gitea/workflows/release.yml` 是流水线配置，内容如下：

```yaml
name: Build and Release

# 当推送 v 开头的 tag 时触发
on:
  push:
    tags:
      - 'v*'

jobs:
  build-and-release:
    runs-on: ubuntu-latest
    steps:
      - name: Checkout
        uses: actions/checkout@v4

      - name: Setup Go
        uses: actions/setup-go@v5
        with:
          go-version-file: 'go.mod'

      - name: Cross-compile
        run: |
          mkdir -p dist
          GOOS=linux   GOARCH=amd64  go build -ldflags="-s -w" -o dist/dhs-linux-amd64   .
          GOOS=linux   GOARCH=arm64  go build -ldflags="-s -w" -o dist/dhs-linux-arm64   .
          GOOS=windows GOARCH=amd64  go build -ldflags="-s -w" -o dist/dhs-windows-amd64.exe .
          GOOS=darwin  GOARCH=amd64  go build -ldflags="-s -w" -o dist/dhs-darwin-amd64  .
          GOOS=darwin  GOARCH=arm64  go build -ldflags="-s -w" -o dist/dhs-darwin-arm64  .
          ls -lh dist/

      - name: Create Release
        uses: https://gitea.com/actions/gitea-release-action@v1
        with:
          name: "${{ gitea.ref_name }}"
          body: |
            ## ${{ gitea.ref_name }}
            自动构建版本，支持以下平台：
            - Windows 64位
            - Linux 64位 / ARM64
            - macOS Intel / Apple Silicon
          files: |
            dist/dhs-linux-amd64
            dist/dhs-linux-arm64
            dist/dhs-windows-amd64.exe
            dist/dhs-darwin-amd64
            dist/dhs-darwin-arm64
          draft: false
          prerelease: false
```

### 2.4 如何使用流水线

配置完成后，每次发版只需：

```bash
# 1. 修改代码、提交
git add -A && git commit -m "feat: 新功能"
git push

# 2. 打 tag 并推送 — 剩下的全自动！
git tag v0.2.0 -m "v0.2.0"
git push origin v0.2.0
```

### 2.5 查看流水线运行状态

- 浏览器：`http://192.168.0.180:9000/study/dhs/actions`
- 点击具体运行记录可看实时日志

---

## 三、两种方式对比

| 对比项 | 手动发布 (tea) | 自动流水线 (Actions) |
|--------|---------------|---------------------|
| 编译环境 | 本地 Windows | Runner 服务器 (Linux) |
| 操作步骤 | 编译 → tag → tea create | 打 tag → push（自动） |
| 适用场景 | 快速测试发布 | 正式版本发布 |
| 前置条件 | 安装 Go + tea | 部署 Runner 一次 |
| 重复性 | 每次手动 3 步 | 一次配置，永久自动 |

> **建议**：日常开发用手动发布快速验证，正式发版靠流水线自动化。
