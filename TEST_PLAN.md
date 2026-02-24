# 测试文档

## 运行方式

```bash
# Go 单元测试 + 集成测试
go test ./internal/... -v          # 单元测试
go test . -v -timeout 30s          # 集成测试（需要 Redis）

# Python 单元测试
cd scripts && python -m unittest test_unit -v
```

---

## 一、Go 单元测试

### 1. internal/config — 配置加载

| 测试方法 | 验证内容 |
|---|---|
| `TestGetActiveEnv_Testnet` | `active_env=testnet` 时返回 testnet 配置 |
| `TestGetActiveEnv_Mainnet` | `active_env=mainnet` 时返回 mainnet 配置 |
| `TestGetActiveEnv_DefaultsToTestnet` | 未知 env 值时回退到 testnet |
| `TestLoadConfig_EnvVarOverride` | 环境变量优先级高于配置文件；未设置的字段保留文件值 |
| `TestLoadConfig_InvalidPath` | 文件不存在时返回 error |
| `TestLoadConfig_InvalidJSON` | JSON 格式错误时返回 error |

**验证方法：** 使用 `os.CreateTemp` 创建临时配置文件，通过 `os.Setenv` 注入环境变量，调用 `LoadConfig` 后断言字段值，`defer` 清理环境变量和临时文件。

---

### 2. internal/binance — API 客户端

| 测试方法 | 验证内容 |
|---|---|
| `TestCreateSignature` | HMAC-SHA256 签名非空；相同输入产生相同签名；不同输入产生不同签名 |
| `TestGetListenKey_Success` | POST 请求携带正确 `X-MBX-APIKEY` Header；返回 `listenKey` 字段 |
| `TestGetListenKey_ServerError` | 服务端返回 401 时返回 error |
| `TestRenewListenKey_Success` | 发送 PUT 请求；`listenKey` 参数正确传入 query string |
| `TestRenewListenKey_Failure` | 服务端返回 400 时返回 error |
| `TestPlaceOrder_Success` | POST 下单成功；返回非空 JSON 结果 |
| `TestPlaceOrder_InsufficientBalance` | 服务端返回 400（余额不足）时返回 error |
| `TestGetPosition_WithPosition` | 正确解析 `positionAmt` 和 `entryPrice` |
| `TestGetPosition_Empty` | 空仓位列表时返回 `"0.0"/"0.0"` |
| `TestNewAPIClient` | APIKey/APISecret 正确赋值；HTTP 超时为 5 秒 |

**验证方法：** 使用 `net/http/httptest.NewServer` 启动 mock HTTP 服务器，将 `c.BaseURL` 指向 mock 地址，断言请求方法、Header、响应解析结果。

---

### 3. internal/orderbook — 本地订单簿

| 测试方法 | 验证内容 |
|---|---|
| `TestNewLocalOrderBook` | 初始状态 `IsReady=false`、`Synced=false` |
| `TestInitWithSnapshot` | 快照加载后 `IsReady=true`、`Synced=false`；`LastUpdateID` 正确；档位数量正确 |
| `TestProcessDepthEvent_PerfectSync` | `FirstUpdateID <= SnapshotID <= FinalUpdateID` 时完美缝合，`Synced=true`，`LastUpdateID` 更新 |
| `TestProcessDepthEvent_DiscardOldEvent` | `FinalUpdateID < LastUpdateID` 的旧事件被静默丢弃 |
| `TestProcessDepthEvent_ForceSync` | `FirstUpdateID > LastUpdateID` 时强行缝合 |
| `TestProcessDepthEvent_SequenceGapTriggersResync` | 缝合后序列号断层时 `IsReady=false`、`NeedsResync=true` |
| `TestProcessDepthEvent_NotReady` | 未初始化快照时处理事件不报错 |
| `TestUpdateLevels_DeleteZeroQty` | 数量为 `"0"` 的档位从 map 中删除 |
| `TestGetTopN_Sorting` | Bids 降序排列；Asks 升序排列 |
| `TestGetTopN_Truncation` | 返回档位数量不超过 N |
| `TestConcurrentAccess` | 50 个并发 goroutine 同时读写不发生 data race（配合 `-race` 标志） |

**验证方法：** 使用 `makeSnapshot` / `makeEvent` 辅助函数构造测试数据，直接操作 `LocalOrderBook` 结构体字段断言状态；并发测试使用 `sync.WaitGroup` 协调。

---

## 二、Python 单元测试

### 4. strategies.SpreadBreakoutStrategy — 价差突破策略

| 测试方法 | 验证内容 |
|---|---|
| `test_no_signal_when_spread_below_threshold` | 价差低于阈值时返回 `None` |
| `test_signal_when_spread_above_threshold` | 价差超过阈值时返回 BUY 信号；price = best_ask + 5.0 |
| `test_no_signal_during_cooldown` | 冷却期内第二次 tick 返回 `None` |
| `test_signal_after_cooldown_expires` | 强制过期 `last_fire_time` 后可再次触发 |
| `test_no_signal_when_position_at_max` | 仓位达到 `max_position` 上限时返回 `None` |
| `test_no_signal_when_position_exceeds_max` | 仓位超过上限时返回 `None` |
| `test_no_signal_empty_book` | 空盘口（bids/asks 为空列表）返回 `None` |
| `test_no_signal_missing_book_keys` | 缺少 b/a 键的盘口返回 `None` |
| `test_tick_count_increments` | 每次 `on_tick` 调用后 `tick_count` 递增 |

**验证方法：** 使用 `make_book(bid, ask)` 辅助函数构造标准盘口字典，直接实例化策略对象，通过操作 `last_fire_time` 控制冷却状态，断言返回值及字段。

---

### 5. macd_strategy.MACD5MinStrategy — MACD 趋势策略

| 测试方法 | 验证内容 |
|---|---|
| `test_stop_loss_long` | 多单浮亏 ≥ 2% 触发止损，返回 SELL 信号，reason=`Hard Stop Loss` |
| `test_take_profit_long` | 多单浮盈 ≥ 5% 触发止盈，返回 SELL 信号，reason=`Hard Take Profit` |
| `test_stop_loss_short` | 空单浮亏 ≥ 2% 触发止损，返回 BUY 信号 |
| `test_take_profit_short` | 空单浮盈 ≥ 5% 触发止盈，返回 BUY 信号 |
| `test_no_risk_trigger_within_range` | 盈亏在范围内不触发风控，进入 MACD 逻辑 |
| `test_macd_golden_cross_open_long` | 无仓位时 trend=1（金叉）开多，reason=`MACD 金叉开多` |
| `test_macd_death_cross_open_short` | 无仓位时 trend=-1（死叉）开空，reason=`MACD 死叉开空` |
| `test_macd_golden_cross_close_short` | 持空单时金叉平空，返回 BUY，reason=`MACD 金叉平空` |
| `test_macd_death_cross_close_long` | 持多单时死叉平多，返回 SELL，reason=`MACD 死叉平多` |
| `test_check_interval_throttle` | `check_interval` 内重复调用返回 `None` |
| `test_get_macd_trend_uses_config_spans` | `fast_span/slow_span/signal_span` 从配置读取，非硬编码 |
| `test_api_url_mainnet` | `active_env=mainnet` 时 API URL 包含 `fapi.binance.com` |
| `test_api_url_testnet` | `active_env=testnet` 时 API URL 包含 `testnet` |

**验证方法：** 使用 `unittest.mock.patch.object` mock `get_macd_trend` 返回值，隔离网络请求；通过精确计算 PnL 边界值（如 entry=50000，bid=49000 → pnl=-2.0%）构造触发/不触发场景。

---

## 三、Go 集成测试

### 6. OrderBook → Redis 数据流

| 测试方法 | 验证内容 |
|---|---|
| `TestOrderBookToRedis` | OrderBook 序列化后写入 Redis，读回反序列化后 symbol 正确；bids 降序、asks 升序 |
| `TestRedisPositionAndBalance` | `Wallet:USDT`、`Position:BTCUSDT`、`EntryPrice:BTCUSDT` 三个 key 写入读取一致 |

**验证方法：** 使用 Redis DB 15 隔离测试数据，测试结束后 `FlushDB` 清理；若 Redis 不可用则 `t.Skip` 跳过。

---

### 7. UDS Socket HTTP 端点

| 测试方法 | 验证内容 |
|---|---|
| `TestUDSOrderEndpoint` | 通过 Unix Domain Socket 发送 POST 订单；服务端正确接收 `side/quantity` 字段；响应 200 |
| `TestUDSOrderEndpoint_RejectNonPost` | GET 请求返回 405 Method Not Allowed |

**验证方法：** 在 `/tmp/test_quant_integration.sock` 启动真实 UDS 监听服务，使用自定义 `DialContext` 的 `http.Client` 发送请求，通过 channel 异步断言服务端收到的 payload。

---

### 8. APIClient 完整订单流程

| 测试方法 | 验证内容 |
|---|---|
| `TestAPIClientFullOrderFlow` | 依次执行：GetListenKey → RenewListenKey → PlaceOrder → GetPosition → CancelOrder，每步均成功 |

**验证方法：** 使用 `httptest.NewServer` 按 URL path 路由 mock 不同接口，将 `c.BaseURL` 指向 mock 服务器，验证完整交易生命周期中各接口的调用顺序和返回值解析。

---

## 四、测试覆盖统计

| 模块 | 测试数 | 结果 |
|---|---|---|
| `internal/config` | 6 | PASS |
| `internal/binance` | 10 | PASS |
| `internal/orderbook` | 11 | PASS |
| `strategies.py` | 9 | PASS |
| `macd_strategy.py` | 13 | PASS |
| 集成测试 | 5 | PASS |
| **合计** | **49** | **全部通过** |
