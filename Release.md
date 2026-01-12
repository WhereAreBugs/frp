## v0.66.0-ext

### 主要特性
- TCPMux 多会话自动择优：支持frpc到frps之间多连接（多协议或单协议），主动/被动链路探测自动选择最优会话，切换对任意客户端 payload 透明。
- 多 FRPS 并行连接与负载均衡：`servers[]` 多服务端注册，按 proxy 的 `serverNames` 允许列表筛选，支持每个服务端独立 token。
- FRPS 反代端口支持 TCP Fast Open：为各类 TCP listener 尝试开启 TFO（尽力而为，不支持则自动降级），通过 `transport.tcpFastOpen` / `transport.tcpFastOpenQueue` 配置，详见 `doc/tcp_fast_open.md`。

### 兼容性
- `tcpMuxLinkProbeMode` 默认 `auto`，会自动检测服务端是否支持主动链路探测模式，不支持时回退被动模式。
- 多 FRPS 模式暂不支持 visitors/webServer/virtualNet，proxy 需配置 `serverNames` 以选择注册到哪些服务端。

### 构建与发布
- 可执行文件命名统一为 `frpc-ext` / `frps-ext`，避免与上游冲突。
- OpenWrt feed 提供 `frpc-ext`、`frps-ext`、`luci-app-frpc-ext` 三个包，支持官方 SDK 多架构编译。
