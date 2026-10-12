# PingTool — 网络连通性批量测试工具

一个用 Go 编写的 **Windows 原生 GUI** 工具，用于批量测试多个地址的连通性与延迟：

- **ICMP ping**：调用 Windows 的 `IcmpSendEcho` / `Icmp6SendEcho2`，**不需要管理员权限**。
- **TCPing**：对真实 TCP 端口发起连接并测量握手耗时。
- **批量测试**：一次可测试任意多个目标，并发执行。
- **网段扫描**：支持 CIDR（`192.168.1.0/24`）与地址范围（`192.168.1.1-254`），可一键导入本机网段。
- **两种运行方式**：
  - **按包数**：每个目标发送固定数量的探测包（例如 50 个）。
  - **按时长**：在指定时长内持续探测（例如 60 秒）。
- **状态格**：连续测试时，每个探测包对应一个小方格 —— 绿色=通，红色=不通，黄色=高延迟。
- **延迟显示**：选中单个目标时显示最新延迟、延迟曲线（折线图）、最小/平均/最大/抖动，方便判断异常高延迟。
- **导出**：可导出汇总 CSV，也可导出包含每个探测包的 **ping 风格详细日志**（含 `icmp_seq`、`ttl`、`time` 等连续输出）。

---

## 下载

前往 [Releases](https://github.com/luolu1/pingtool/releases) 下载现成的 Windows 版本：

| 文件 | 适用平台 |
| --- | --- |
| `PingTool-windows-amd64.exe` | 64 位 Windows（Intel / AMD） |
| `PingTool-windows-arm64.exe` | Windows on ARM（骁龙等 ARM64 设备） |

下载后直接双击运行，无需安装、无需管理员权限。

---

## 界面说明

| 区域 | 说明 |
| --- | --- |
| ① 目标地址 | 每行一个地址，支持注释、批量输入、网段与范围扫描。 |
| ② 测试参数 | 测试方式（按包数／按时长）、间隔、超时、并发数、高延迟告警阈值。 |
| 表格 | 每个目标的协议、已发／成功／失败、丢包率、最新／最小／平均／最大延迟、状态。 |
| ③ 选中目标详情 | 大号最新延迟、状态格、延迟曲线（折线图）、统计信息与最近一次错误。 |
| 底部状态栏 | 汇总：目标数、连通／不通数量、总发送／收到、总丢包率、当前阈值。 |

图例：<span>■</span> 正常（绿） · <span>■</span> 高延迟（黄，超过阈值） · <span>■</span> 不通（红）。

---

## 目标地址语法

```
# 以 # 开头的行为注释
8.8.8.8                 # 纯 IP，默认走 ICMP
example.com             # 域名，默认走 ICMP
1.1.1.1:443             # 带端口 => TCPing
www.baidu.com:443       # 域名 + 端口 => TCPing
[2001:db8::1]:443       # IPv6 带端口
tcp://example.com:80    # 强制 TCP
icmp://8.8.8.8          # 强制 ICMP
```

- 程序按 **自动** 方式识别协议（推荐）：`192.168.1.1` → ICMP；`192.168.1.1:22` → TCPing。这样一行一个地址即可批量导入。
- 需要指定协议时，使用 `tcp://` / `icmp://` 前缀即可。

### 网段 ping（子网扫描）

一行即可展开成整段地址，每个地址独立探测、独立统计：

```
192.168.1.0/24              # IPv4 网段（自动跳过网络号 .0 与广播地址 .255）
10.0.0.0/22                 # 更大网段（最多 65536 个地址）
192.168.1.0/24:22           # 网段 + 端口 => 对整段做 TCPing
tcp://192.168.1.0/24:3389   # 强制 TCP 的网段扫描
192.168.1.1-254             # 最后一个字节的范围
192.168.1.1-192.168.1.100   # 完整地址范围（支持跨字节）
2001:db8::/120              # IPv6 网段
```

- **本机网段** 按钮：自动把本机所有 IPv4 网卡的网段（CIDR）追加到目标列表，无需手动输入。
- 单个表达式的展开上限为 **65536** 个地址；`10.0.0.0/8` 这类过大的网段会被拒绝并提示缩小范围。
- 网段默认走 ICMP；在末尾加 `:端口`（或在行首写 `tcp://`）即改为对整个网段做 TCPing。

---

## 构建

### 环境要求

- Go 1.27 或更高版本（`go.mod` 声明 `go 1.27.2`）。
- 目标平台为 Windows（`github.com/lxn/walk` 仅支持 Windows）。
- 支持 `amd64` 与 `arm64` 两种 Windows 架构（交叉编译即可，无需在 Windows 上构建）。

### 直接构建

```powershell
GOOS=windows GOARCH=amd64 go build -ldflags "-H windowsgui -s -w" -o PingTool-windows-amd64.exe .
```

`-H windowsgui` 让生成的程序成为无控制台窗口的 GUI 程序；构建 ARM64 版本时把 `GOARCH` 改为 `arm64` 即可。

### 带图标与清单构建（推荐）

图标和应用程序清单（DPI 感知、Common-Controls v6）通过 `rsrc` 嵌入：

```powershell
# 仅首次需要安装
go install github.com/akavel/rsrc@latest

# 生成 app.ico（纯 Go，无需额外工具）
go run ./tools/genicon -o app.ico

# 生成资源 .syso，之后 go build 会自动嵌入
rsrc -manifest pingtool.manifest -ico app.ico -arch amd64 -o rsrc_windows_amd64.syso

GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "-H windowsgui -s -w" -o PingTool-windows-amd64.exe .
```

### 使用 Makefile

在安装有 `make` 的环境（Git Bash / MSYS2 / WSL）中：

```bash
make build        # 生成 PingTool-windows-amd64.exe
make build-arm64  # 生成 PingTool-windows-arm64.exe
make build-386    # 生成 32 位版本 PingTool-windows-386.exe
make icon         # 重新生成 app.ico
make test         # 运行单元测试
make vet          # go vet
make dist         # 生成 dist/ 发布目录
```

### 自动发布

仓库内置 GitHub Actions 工作流 `.github/workflows/release.yml`：推送一个 `v*` 形式的标签（例如 `v1.0.0`）后，会自动构建 `amd64` 与 `arm64` 两个 Windows 版本并创建 / 更新对应的 GitHub Release。

```bash
git tag v1.0.0
git push origin v1.0.0
```

---

## 运行

双击 `PingTool-windows-amd64.exe`（ARM 设备用 `PingTool-windows-arm64.exe`）即可，无需管理员权限。

1. 在「目标地址」中粘贴或从文件载入地址列表（每行一个）。
2. 设置测试方式（按包数／按时长）、间隔、超时、并发数等参数。
3. 点击 **▶ 开始测试**。测试过程中可点击 **■ 停止** 提前结束。
4. 在表格中点击任意一行，右侧会显示该目标的详细延迟、状态格与延迟曲线。
5. 点击 **导出结果 CSV…** 保存汇总表；点击 **导出详细日志…** 保存 ping 风格的逐包连续输出。

---

## 说明与限制

- 本工具仅支持 Windows。非 Windows 平台的 `main_stub.go` 只是为了让 `go build ./...` 与单元测试可以在开发机上运行，它不会启动 GUI。
- ICMP 使用 Win32 API，因此 **无需管理员权限**；TCPing 使用普通 TCP 连接。
- 高延迟告警阈值设为 `0` 表示关闭黄色高亮。
- 源码中探测引擎 `internal/pinger` 与界面完全解耦，可单独测试。
- Linux / macOS 目前没有发布版本：界面基于仅支持 Windows 的 `lxn/walk`。

---

## 目录结构

```
pingtool/
├── app.go                     # GUI 主窗口、事件处理、刷新循环
├── export.go                  # ping 风格详细日志的格式化（平台无关，可测试）
├── grid.go                    # 状态格与延迟曲线的绘制
├── model.go                   # 表格数据模型
├── main_stub.go               # 非 Windows 平台占位
├── pingtool.manifest          # 应用程序清单
├── app.ico                    # 图标（*.syso 由 make res-<arch> 生成）
├── .github/workflows/release.yml  # 自动构建并发布 Windows amd64 / arm64
├── tools/genicon/             # 纯 Go 图标生成器
└── internal/pinger/           # 探测引擎
    ├── pinger.go              # 配置、目标解析、并发引擎、TCP 探测
    ├── icmp_windows.go        # Windows IcmpSendEcho 实现
    └── icmp_unix.go           # 非 Windows 回退实现（仅开发用）
```
