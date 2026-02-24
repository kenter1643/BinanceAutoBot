# ⚡️ Go-Python 极速量化交易框架 (Binance Futures)

这是一个为币安合约（USDT-M）设计的高频/趋势量化交易底层框架。系统采用 **"Go 负责极速 I/O 与执行，Python 负责策略与算力"** 的非对称异构架构，彻底剥离了网络通信与策略计算的耦合。

通过 **Redis 内存切片共享** 与 **UDS (Unix Domain Socket) 底层进程间通信**，实现了微秒级的本地跨语言调用，完美规避了传统 HTTP/TCP 协议栈的开销。

## 🏗 核心架构图

**架构数据流转说明：**
1. **下行链路 (Data 进)**：Go 网关通过 WebSocket 保持与 Binance 撮合引擎的连接。包含公共盘口 `@depth` 的强行缝合机制，以及私有资产 `UserData` 的自动重连机制。所有清洗后的数据 100ms 级覆写进 Redis。
2. **决策链路 (Brain)**：Python 策略引擎毫秒级轮询 Redis，获取最新切片（OrderBook、真实仓位、开仓均价、钱包余额），并结合 Pandas 处理的 K 线数据，进行多维状态机计算。
3. **上行链路 (Order 出)**：策略一旦决断，Python 通过底层的 UDS 瞬间将指令拍给 Go 网关。Go 网关接管复杂的 HMAC-SHA256 签名计算，通过保活的 HTTP 连接极速打向 Binance API 完成交易。

## ✨ 核心工业级特性

- **🛡️ 状态兜底与自愈**：启动时主动发起 REST 请求拉取全量资金/仓位/均价作为基线，随后由 WS 增量接管。遇网络闪断（如 1006 EOF），网关自动完成无限次重连断线恢复（指数退避，上限 60s）。
- **💓 ListenKey 守护神**：Go 后台协程每 30 分钟自动发送 PUT 请求保活，防止私有 WebSocket 被币安强制踢下线。
- **🔄 OrderBook 自动重同步**：序列号断层时自动标记并重新拉取 REST 快照，保证盘口数据始终连续一致。
- **⚙️ 纯粹的配置驱动**：交易标的、策略选择、MACD 敏感度、动态滑点、绝对止盈止损比例全部在 `config.json` 中配置，无需改动核心逻辑代码。API Key 通过环境变量注入，不写入代码库。
- **⚔️ 独立硬核风控**：策略内部包含最高优先级的 TP/SL（止盈止损）拦截器，基于 Tick 级盘口实时计算浮亏，防止因指标滞后导致的插针爆仓。
- **🧪 完整测试覆盖**：49 个测试（Go 单元/集成 + Python 单元），覆盖签名、OrderBook 状态机、策略四象限、UDS 端到端等核心路径。

## 📂 目录结构

```text
BinanceAutoBot2/
├── config.json                 # 统一全局配置文件 (策略参数，API Key 通过环境变量注入)
├── .gitignore                  # 防止 config.json 等敏感文件提交
├── CHANGELOG.md                # 变更记录
├── TEST_PLAN.md                # 测试文档
├── integration_test.go         # Go 集成测试
├── cmd/
│   ├── binance-gateway/        # [核心] Go 极速网关主程序
│   └── test-order/             # [测试] 独立发单测试脚本
├── internal/
│   ├── binance/
│   │   ├── api_client.go       # REST API (发单、快照、ListenKey 续期)
│   │   ├── api_client_test.go  # API 客户端单元测试
│   │   ├── user_stream.go      # 私有资产推送 (指数退避重连)
│   │   ├── ws_client.go        # 公共行情推送 (指数退避重连)
│   │   └── types.go            # 数据结构定义
│   ├── config/
│   │   ├── config.go           # 配置解析 (环境变量优先)
│   │   └── config_test.go      # 配置单元测试
│   └── orderbook/
│       ├── local_ob.go         # 盘口状态机 (断层自动重同步)
│       └── local_ob_test.go    # OrderBook 单元测试
└── scripts/                    # Python 策略大脑目录
    ├── main_engine.py          # [核心] Python 量化主引擎
    ├── strategies.py           # 策略基类模块
    ├── macd_strategy.py        # [策略] 5 分钟 MACD 趋势策略 (带硬风控)
    ├── test_uds_order.py       # [测试] UDS 连通性测试
    └── test_unit.py            # Python 单元测试
```

## 🔐 安全配置

API Key 不写入 `config.json`，通过环境变量注入：

```bash
export BINANCE_TESTNET_API_KEY="your_testnet_api_key"
export BINANCE_TESTNET_API_SECRET="your_testnet_api_secret"
export BINANCE_MAINNET_API_KEY="your_mainnet_api_key"
export BINANCE_MAINNET_API_SECRET="your_mainnet_api_secret"
```

## 🚀 极速启动指南

### 第一步：环境与依赖准备

1. **启动 Redis 服务**：确保本地或服务器已运行 Redis 进程（默认监听 `127.0.0.1:6379`）。
2. **配置 API 密钥**：通过上方环境变量注入，或直接填入 `config.json`（勿提交到 git）。
3. **构建 Python 算法沙盒**：

   ```bash
   conda create -n quant_engine python=3.10 -y
   conda activate quant_engine
   pip install redis requests-unixsocket pandas requests
   ```

### 第二步：编译并启动 Go 网关

```bash
go build ./cmd/binance-gateway/
./binance-gateway
```

### 第三步：启动 Python 策略引擎

```bash
conda activate quant_engine
cd scripts
python main_engine.py
```

## 🧪 运行测试

```bash
# Go 单元测试
go test ./internal/... -v

# Go 集成测试（需要 Redis）
go test . -v -timeout 30s

# Python 单元测试
cd scripts && python -m unittest test_unit -v
```

详细测试说明见 [TEST_PLAN.md](TEST_PLAN.md)。
