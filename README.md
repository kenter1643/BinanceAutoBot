# ⚡️ Go-Python 极速量化交易框架 (Binance Futures)

这是一个为币安合约（USDT-M）设计的高频/趋势量化交易底层框架。系统采用 **“Go 负责极速 I/O 与执行，Python 负责策略与算力”** 的非对称异构架构，彻底剥离了网络通信与策略计算的耦合。

通过 **Redis 内存切片共享** 与 **UDS (Unix Domain Socket) 底层进程间通信**，实现了微秒级的本地跨语言调用，完美规避了传统 HTTP/TCP 协议栈的开销。

## 🏗 核心架构图



**架构数据流转说明：**
1. **下行链路 (Data 进)**：Go 网关通过 WebSocket 保持与 Binance 撮合引擎的连接。包含公共盘口 `@depth` 的强行缝合机制，以及私有资产 `UserData` 的自动重连机制。所有清洗后的数据 100ms 级覆写进 Redis。
2. **决策链路 (Brain)**：Python 策略引擎毫秒级轮询 Redis，获取最新切片（OrderBook、真实仓位、开仓均价、钱包余额），并结合 Pandas 处理的 K 线数据，进行多维状态机计算。
3. **上行链路 (Order 出)**：策略一旦决断，Python 通过底层的 UDS 瞬间将指令拍给 Go 网关。Go 网关接管复杂的 HMAC-SHA256 签名计算，通过保活的 HTTP 连接极速打向 Binance API 完成交易。

## ✨ 核心工业级特性

- **🛡️ 状态兜底与自愈**：启动时主动发起 REST 请求拉取全量资金/仓位/均价作为基线，随后由 WS 增量接管。遇网络闪断（如 1006 EOF），网关自动完成无限次重连断线恢复。
- **💓 ListenKey 守护神**：Go 后台协程每 30 分钟自动发送 PUT 请求保活，防止私有 WebSocket 被币安强制踢下线。
- **⚙️ 纯粹的配置驱动**：交易标的、策略选择、MACD 敏感度、动态滑点、绝对止盈止损比例全部在 `config.json` 中配置，无需改动核心逻辑代码。
- **⚔️ 独立硬核风控**：策略内部包含最高优先级的 TP/SL（止盈止损）拦截器，基于 Tick 级盘口实时计算浮亏，防止因指标滞后导致的插针爆仓。

## 📂 目录结构

```text
awesomeProject/
├── config.json                 # 统一全局配置文件 (API Key, Redis, 策略参数)
├── cmd/
│   ├── binance-gateway/        # [核心] Go 极速网关主程序
│   ├── test-order/             # [测试] 独立发单测试脚本
│   └── test-balance/           # [测试] 独立资产查询脚本
├── internal/
│   ├── binance/
│   │   ├── api_client.go       # REST API (发单、快照、初始仓位/资金查询、ListenKey续期)
│   │   ├── user_stream.go      # 私有资产推送 (带断线自动重连机制)
│   │   └── ws_client.go        # 公共行情推送
│   ├── config/                 # 配置解析模块
│   └── orderbook/
│       └── local_ob.go         # 盘口状态机 (断层强行缝合)
└── scripts/                    # Python 策略大脑目录
    ├── main_engine.py          # [核心] Python 量化主引擎 (连接 Redis, 驱动策略, UDS 通信)
    ├── strategies.py           # 策略基类模块
    ├── macd_strategy.py        # [策略] 基于 Pandas 的 5 分钟 MACD 趋势策略 (带硬风控)
    └── test_uds_order.py       # [测试] UDS 连通性测试
```

## 🚀 极速启动指南

### 第一步：环境与依赖准备

1. **启动 Redis 服务**：确保本地或服务器已运行 Redis 进程（默认监听 `127.0.0.1:6379`）。
2. **配置 API 密钥**：打开项目根目录的 `config.json`，在对应的 `testnet` 或 `mainnet` 节点填入你的 Binance API Key 与 Secret。
3. **构建 Python 算法沙盒**：
   强烈推荐使用 Conda 隔离环境，避免污染系统原生的 Python 环境：
   ```bash
   # 1. 创建名为 quant_engine 的纯净 Python 3.10 环境
   conda create -n quant_engine python=3.10 -y
   
   # 2. 激活该虚拟环境
   conda activate quant_engine
   
   # 3. 极速安装量化运算与底层 IPC 通信依赖包
   pip install redis requests-unixsocket pandas requests
   ```

