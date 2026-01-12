# TCP Fast Open (TFO)

`frps` can enable TCP Fast Open (TFO) on its TCP listeners to reduce connection establishment latency.

This is a **best-effort** feature:

- Support depends on the operating system and kernel settings.
- When unsupported or when enabling fails, `frps` will **fall back to normal TCP** and still start normally.

## Configuration

Enable TFO on `frps`:

```toml
# frps.toml
transport.tcpFastOpen = true
# Queue length (Linux). Ignored or treated as a boolean on some platforms.
transport.tcpFastOpenQueue = 1024
```

## Which listeners are affected?

When enabled, `frps` will attempt to enable TFO for TCP listeners created via `net.Listen("tcp", ...)`, including (but not limited to):

- `bindPort` (the main `frps` TCP listener)
- TCP proxy `remotePort` listeners
- vhost HTTP/HTTPS listeners when they are on dedicated ports
- `tcpmuxHTTPConnectPort` listener

## Platform notes

- Linux typically also requires enabling the kernel switch (e.g. `net.ipv4.tcp_fastopen`).
- On BSD/macOS, the availability and semantics of `TCP_FASTOPEN` may differ.

If you are targeting high-latency networks, you may want to combine this with other latency-related features, such as TCPMux and connection pooling.
