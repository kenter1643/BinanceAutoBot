# ⚡️ Go-Python 极速量化自动交易机器人Bot框架 (Binance Auto Bot)

这是一个为币安合约（USDT-M）设计的高频/中高频量化交易底层框架。系统采用 **“Go 负责极速 I/O 与执行，Python 负责策略与算力”** 的非对称异构架构，彻底剥离了网络通信与策略计算的耦合。

通过 **Redis 内存切片共享** 与 **UDS (Unix Domain Socket) 底层进程间通信**，实现了微秒级的本地跨语言调用，完美规避了传统 HTTP/TCP 协议栈的开销。

## 🏗 核心架构图

```text
 [ 币安 Binance 撮合引擎 ]
       │            ▲
       │(WS推送)    │(REST极速发单)
       ▼            │
 ┌────────────────────────────────┐
 │       Go 行情与执行网关          │
 │ 1. 维护 Local OrderBook 状态机   │
 │ 2. 监听私有 UserData 仓位/资产    │
 │ 3. 极速签名与订单路由             │
 │ 4. 监听 UDS 本地极速通道          │
 └──────┬──────────────────▲──────┘
        │(高频全量覆写)    │(UDS IPC 二进制/JSON 通信)
        ▼                  │
 ┌──────────────┐          │
 │   Redis      │          │
 │ (内存共享桥)   │          │
 └──────┬───────┘          │
        │(极速微秒级轮询)    │
        ▼                  │
 ┌─────────────────────────┴──────┐
 │       Python 策略大脑引擎        │
 │ 1. 毫秒级读取 OrderBook & 仓位   │
 │ 2. 执行模块化策略 (如价差突破)     │
 │ 3. 风控状态机 (冷却锁/仓位锁)      │
 └────────────────────────────────┘

```

## 📂 目录结构与模块说明

```text
BinanceAutoBot/
├── config.json                 # 统一全局配置文件 (API Key, Redis 地址, 环境切换)
├── cmd/
│   ├── binance-gateway/
│   │   └── main.go             # [核心入口] Go 极速网关主程序
│   └── test-order/
│       └── main.go             # [测试工具] 独立发单测试脚本
├── internal/
│   ├── binance/
│   │   ├── api_client.go       # Binance REST API 客户端 (发单、获取快照、获取 ListenKey)
│   │   ├── user_stream.go      # 私有资产推送通道 (UserData Stream 监听)
│   │   └── ws_client.go        # 公共行情推送通道 (@depth 深度监听)
│   ├── config/
│   │   └── config.go           # 配置加载模块
│   └── orderbook/
│       └── local_ob.go         # 本地盘口状态机 (负责 REST 快照与 WS 增量的完美/强行缝合)
└── scripts/                    # Python 策略大脑目录
    ├── main_engine.py          # [核心入口] Python 量化主引擎 (连接 Redis, 驱动策略, 发送 UDS 指令)
    ├── strategies.py           # 策略基类与具体策略模块 (包含 SpreadBreakoutStrategy)
    └── test_uds_order.py       # [测试工具] Python UDS 发单连通性测试

```

## 🧠 核心组件详解

### 1. Go 行情与执行网关 (`cmd/binance-gateway/main.go`)

* **千里眼**：启动 WebSocket 连接币安 `@depth` 频道，拉取 REST 盘口快照，在本地内存中完成数据流的缝合（支持断层强行缝合），并以 `100ms` 级别的高频向 Redis 覆写最新的 `OrderBook:BTCUSDT`。
* **痛觉神经**：向币安申请 `ListenKey`，建立 `UserData Stream` 私有通道，实时监听订单成交与资产变动，将 `Wallet:USDT` 和 `Position:BTCUSDT` 同步至 Redis。
* **极速机械臂**：在本地监听 Unix Domain Socket (`/tmp/quant_engine.sock`)。一旦收到 Python 传来的极简发单指令（Side, Quantity, Price），立刻在 Go 内部完成 HMAC-SHA256 签名，并通过长连接极速打向币安 API。

### 2. Python 策略主引擎 (`scripts/main_engine.py`)

* 采用模块化设计，负责初始化 Redis 连接和 UDS 会话。
* 包含极速轮询循环（`while True`），以极低的 CPU 消耗读取 Redis 中的盘口切片与真实仓位。
* 将数据喂给挂载的策略模块，一旦策略返回 `signal`，立即通过 `requests-unixsocket` 向 Go 网关开火。

### 3. 策略模块与风控 (`scripts/strategies.py`)

* 实现了一个极简的面向对象策略接口 `BaseStrategy`。
* 内置 **微观价差突破策略 (SpreadBreakoutStrategy)**：
* **触发条件**：实时判断盘口买一卖一价差（Spread）是否大于设定阈值（Threshold）。
* **激进吃单**：条件成立时，计算目标价格（如 `best_ask + 5.0`）以 Taker 身份强吃。
* **时间风控**：内置 `cooldown` 冷却锁，开火后强制休眠 N 秒，防止局部高频震荡导致多重发单。
* **空间风控**：读取实时仓位 `current_position`，一旦超过设定的 `max_position`，立刻锁死扳机，拒绝买入，彻底防止爆仓。



## 🚀 运行指南

### 前置准备

1. 确保本地已运行 Redis 服务（默认 `127.0.0.1:6379`）。
2. 在 `config.json` 中配置好币安 Testnet / Mainnet 的 API Key 与 Secret。
3. Python 环境需安装依赖：`pip install redis requests-unixsocket`。

### 启动全闭环系统

**步骤一：启动 Go 底层网关**
打开终端 A，启动 Go 进程：

```bash
go run ./cmd/binance-gateway/main.go

```

*(观察日志，确保 Redis 连接成功、WS 快照缝合成功、且 `/tmp/quant_engine.sock` 监听启动。)*

**步骤二：启动 Python 策略大脑**
打开终端 B（激活 Conda 虚拟环境），启动主引擎：

```bash
python scripts/main_engine.py

```

*(引擎将接管盘口数据流，并根据 `strategies.py` 中配置的阈值自动捕捉机会执行交易。)*

## ⚠️ 注意事项

* **Socket 权限**：UDS 文件 `/tmp/quant_engine.sock` 会在 Go 启动时自动清理并重建。如果出现 Python 无法写入的情况，请检查文件权限。
* **测试网流动性**：币安 Testnet 的盘口可能长期无交易导致 WS 挂起。可在网页端手动下单一笔，触发 Go 网关的“强行缝合”机制。
* **实盘风控**：目前的策略逻辑为纯多头（Long-only）激进吃单演示。切换至主网前，务必在 `strategies.py` 中补充平仓逻辑与止损逻辑。

