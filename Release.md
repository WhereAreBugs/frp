## v0.66.0-ext

### 新增特性
- 多 FRPS 并行连接（active-active）：支持通过 `servers[]` 同时连接多个 frps，并为每个 server 单独配置 token。
- 按条目选择目标 FRPS：
  - proxies 支持 `serverNames`（允许列表）决定注册到哪些 frps。
  - visitors 支持 `frpsName`（强制单选）绑定到指定 frps。
- 单个 frps 支持多协议端口：`servers[]` 支持 `tcpPort/quicPort/kcpPort/websocketPort/wssPort`，frpc 会为每个协议建连接并在 workConn 上做轮询。
- FRPS TCP Fast Open（TFO）：为 frps 的 TCP listeners 尝试开启 TFO（best-effort，不支持自动降级），配置项：
  - `transport.tcpFastOpen`
  - `transport.tcpFastOpenQueue`
  详见 `doc/tcp_fast_open.md`。

### bug修复
- 修复 frpc 在 `transport.protocol = "kcp"` 时可能卡在连接阶段不超时的问题：KCP dial 现在支持 context/timeout，避免长期阻塞导致代理一直无法注册。

### 兼容性与行为变更
- `tcpMuxLinkProbeMode` 默认 `auto`：自动探测服务端是否支持主动链路探测，不支持则回退被动模式。
- multi-frps 模式限制：
  - webServer / virtualNet 仍不支持。
  - visitors 在 multi-frps 下必须显式填写 `frpsName`，否则校验失败。

### 构建与发布
- 可执行文件命名统一为 `frpc-ext` / `frps-ext`，避免与上游冲突。
- 提供OpenWrt [feed](https://github.com/WhereAreBugs/frp-ext-feed) ，包含 `frpc-ext`、`frps-ext`、`luci-app-frpc-ext` 等包，支持官方 SDK 多架构编译。
