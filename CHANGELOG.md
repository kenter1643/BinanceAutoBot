# Changelog

## [Unreleased] - 2026-02-24

### 安全修复

- **[严重] 移除明文 API Key** — `config.json` 中的 mainnet/testnet API Key 和 Secret 已清空，改为通过环境变量注入：
  - `BINANCE_MAINNET_API_KEY`
  - `BINANCE_MAINNET_API_SECRET`
  - `BINANCE_TESTNET_API_KEY`
  - `BINANCE_TESTNET_API_SECRET`
  - 涉及文件：`internal/config/config.go`, `config.json`

- **[严重] 限制 UDS Socket 文件权限** — `/tmp/quant_engine.sock` 创建后立即设置权限为 `0600`
  - 涉及文件：`cmd/binance-gateway/main.go`

- **[严重] 新增 .gitignore** — 将 `config.json` 加入忽略列表
  - 涉及文件：`.gitignore`（新增）

### 功能修复

- **[严重] 实现 ListenKey 自动续期** — 新增 `RenewListenKey` 方法，每 30 分钟续期一次，修复私有流 60 分钟后断开的问题
  - 涉及文件：`internal/binance/api_client.go`, `cmd/binance-gateway/main.go`

- **[高] Python 主循环异常捕获** — `run()` 内层循环新增 `try/except`，单次 tick 异常不再导致整个引擎崩溃
  - 涉及文件：`scripts/main_engine.py`

- **[高] OrderBook 序列号断层自动重同步** — 断层时标记 `NeedsResync`，Redis 刷盘协程检测到后自动重新拉取 REST 快照，新增线程安全的 `CheckAndClearResync()` 方法
  - 涉及文件：`internal/orderbook/local_ob.go`, `cmd/binance-gateway/main.go`

- **[中] WebSocket 重连指数退避** — `WSClient` 和 `StartUserDataStream` 的重连延迟从固定 2-3 秒改为指数退避（上限 60 秒），避免触发 Binance 限流
  - 涉及文件：`internal/binance/ws_client.go`, `internal/binance/user_stream.go`

- **[中] Redis 操作加超时** — 刷盘的 `rdb.Set` 使用 200ms 超时 context，防止 Redis 阻塞卡死主循环
  - 涉及文件：`cmd/binance-gateway/main.go`

- **[中] MACD 计算使用配置参数** — `get_macd_trend()` 中的 EMA span 从硬编码 12/26/9 改为读取 `self.fast_span` / `self.slow_span` / `self.signal_span`
  - 涉及文件：`scripts/macd_strategy.py`

---

## [d224387] - 基本库

- 初始化项目基础库

## [b770af5] - Initial commit

- 项目初始提交
