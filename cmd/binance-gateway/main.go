package main

import (
	"context"
	"encoding/json"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"BinanceAutoBot2/internal/binance"
	"BinanceAutoBot2/internal/config"
	"BinanceAutoBot2/internal/orderbook"

	"github.com/redis/go-redis/v9"
)

// LocalCommandReq 接收来自 Python 大脑的极简指令
type LocalCommandReq struct {
	Side     string  `json:"side"`     // "BUY" 或 "SELL"
	Quantity float64 `json:"quantity"` // 下单数量
	Price    float64 `json:"price"`    // 下单价格
}

func main() {
	// 1. 加载配置
	cfg, err := config.LoadConfig("config.json")
	if err != nil {
		log.Fatalf("[Main] 读取配置失败: %v", err)
	}
	activeEnv := cfg.Binance.GetActiveEnv()
	symbol := cfg.Binance.Symbol

	log.Printf("🚀 Starting Binance Gateway [%s] for %s...", cfg.Binance.ActiveEnv, symbol)

	// ==========================================
	// 🚨 修复点：在这里初始化 apiClient！
	// ==========================================
	apiClient := binance.NewAPIClient(activeEnv.APIKey, activeEnv.APISecret)
	apiClient.BaseURL = activeEnv.RestBaseURL
	// ==========================================

	// 2. 初始化 Redis
	rdb := redis.NewClient(&redis.Options{Addr: cfg.Redis.Addr, DB: cfg.Redis.DB})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := rdb.Ping(ctx).Err(); err != nil {
		log.Fatalf("[Main] Redis 连接失败: %v", err)
	}
	log.Println("[Main] ✅ Redis connected.")

	// ==========================================
	// 🌟 新增优化：系统启动时，主动拉取一次真实余额进行“兜底初始化”
	// 彻底解决系统刚启动时 Redis 里没有资金数据的真空期问题
	// ==========================================
	if initialBalance, err := apiClient.GetUSDTBalance(); err == nil {
		// 直接将查询到的初始余额刷入 Redis
		_ = rdb.Set(ctx, "Wallet:USDT", initialBalance, 0).Err()
		log.Printf("[Main] 💰 初始资金盘点完成: 当前可用 USDT = %s", initialBalance)
	} else {
		log.Printf("[Main] ⚠️ 初始资金盘点失败: %v", err)
	}
	// ==========================================

	// ==========================================
	// 🌟 新增：初始仓位兜底盘点
	// ==========================================
	// 🌟 修改處 1：初始倉位兜底盤點 (約 80 行附近)
	if initialPos, initialEp, err := apiClient.GetPosition(symbol); err == nil {
		_ = rdb.Set(ctx, "Position:"+symbol, initialPos, 0).Err()
		_ = rdb.Set(ctx, "EntryPrice:"+symbol, initialEp, 0).Err() // 寫入均價
		log.Printf("[Main] 📦 初始倉位盤點: %s 持倉 = %s | 均價 = %s", symbol, initialPos, initialEp)
	} else {
		log.Printf("[Main] ⚠️ 初始仓位盘点失败: %v", err)
	}
	// ==========================================

	// ==========================================
	// 🌟 新增：启动私有资产监听通道，并同步至 Redis
	// ==========================================
	listenKey, err := apiClient.GetListenKey()
	if err != nil {
		log.Printf("[Main] ⚠️ 获取 ListenKey 失败 (可能 API Key 权限不足): %v", err)
	} else {
		// 动态判断当前环境的 WebSocket 域名
		wsBase := "wss://stream.binancefuture.com/ws/" // 默认测试网
		if cfg.Binance.ActiveEnv == "mainnet" {
			wsBase = "wss://fstream.binance.com/ws/" // 主网
		}
		userDataWSURL := wsBase + listenKey

		// 🌟 修复点：将 event.Event 改为 event.EventType
		go binance.StartUserDataStream(ctx, userDataWSURL, func(event binance.UserDataEvent) {
			// 🌟 把 event.Event 改成 event.EventType
			if event.EventType == "ACCOUNT_UPDATE" {
				// 1. 同步最新钱包余额
				for _, bal := range event.Account.Balances {
					if bal.Asset == "USDT" {
						_ = rdb.Set(ctx, "Wallet:USDT", bal.Balance, 0).Err()
						log.Printf("💰 [Redis同步] 余额覆写 -> USDT: %s", bal.Balance)
					}
				}

				// 2. 同步最新仓位与均价
				for _, pos := range event.Account.Positions {
					if pos.Symbol == symbol {
						_ = rdb.Set(ctx, "Position:"+symbol, pos.Amount, 0).Err()
						_ = rdb.Set(ctx, "EntryPrice:"+symbol, pos.EntryPrice, 0).Err()
						log.Printf("💾 [Redis同步] 仓位覆写 -> %s: 数量 %s (均价: %s)", symbol, pos.Amount, pos.EntryPrice)
					}
				}
			}
		})

		// 每 30 分钟续期 ListenKey，防止 60 分钟后私有流断开
		go func() {
			ticker := time.NewTicker(30 * time.Minute)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					if err := apiClient.RenewListenKey(listenKey); err != nil {
						log.Printf("[Main] ⚠️ ListenKey 续期失败: %v", err)
					} else {
						log.Printf("[Main] ✅ ListenKey 续期成功")
					}
				}
			}
		}()

		// ==========================================
		// 🛡️ 新增：企業級狀態對帳協程 (State Reconciliation)
		// 目的：每 5 分鐘強制拉取一次 REST API 真實狀態，防止 WS 漏接導致的「幽靈倉位」
		// ==========================================
		go func() {
			// 設定每 5 分鐘對帳一次 (頻率不要太高，以免消耗 API 權重)
			ticker := time.NewTicker(5 * time.Minute)
			defer ticker.Stop()

			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					log.Println("⏱️ [定時對帳] 啟動 REST API 狀態兜底同步...")

					// 1. 強制核對並覆寫錢包餘額
					if bal, err := apiClient.GetUSDTBalance(); err == nil {
						_ = rdb.Set(ctx, "Wallet:USDT", bal, 0).Err()
					} else {
						log.Printf("⚠️ [定時對帳] 餘額同步失敗: %v", err)
					}

					// 2. 強制核對並覆寫真實倉位與均價
					if posAmount, posEp, err := apiClient.GetPosition(symbol); err == nil {
						_ = rdb.Set(ctx, "Position:"+symbol, posAmount, 0).Err()
						_ = rdb.Set(ctx, "EntryPrice:"+symbol, posEp, 0).Err()
						// log.Printf("⏱️ [定時對帳] 倉位核對完成 -> %s: 數量 %s", symbol, posAmount) // 怕日誌太吵可以註解掉這行
					} else {
						log.Printf("⚠️ [定時對帳] 倉位同步失敗: %v", err)
					}
				}
			}
		}()
		// ==========================================
	}
	// ==========================================

	// 3. 启动行情状态机
	// 3. 启动行情状态机 (🌟 升级为完全事件驱动的零延迟架构)
	ob := orderbook.NewLocalOrderBook(symbol)
	redisKey := "OrderBook:" + symbol

	wsClient := &binance.WSClient{
		URL: activeEnv.WSDepthURL,
		OnDepthFunc: func(event binance.WSDepthEvent) {
			// 1. 毫秒级处理增量事件
			_ = ob.ProcessDepthEvent(event)

			// 2. 线程安全地检测序列号断层，重新拉取快照
			if ob.CheckAndClearResync() {
				log.Printf("[Main] 🔄 OrderBook 断层，重新拉取快照...")
				if snap, err := binance.GetDepthSnapshot(activeEnv.RestBaseURL, symbol, 1000); err == nil {
					ob.InitWithSnapshot(snap)
				}
				return
			}

			// 3. 🌟 绝对的零延迟：只要状态机 Ready，立马刷入 Redis！不等任何 Ticker！
			if ob.IsReady && ob.Synced {
				data, _ := json.Marshal(ob.GetTopN(20))
				// 使用一个极短的 context 防止 Redis 阻塞 WS 接收协程
				rCtx, rCancel := context.WithTimeout(ctx, 50*time.Millisecond)
				_ = rdb.Set(rCtx, redisKey, data, 0).Err()
				rCancel()
			}
		},
	}
	go wsClient.Start(ctx)

	time.Sleep(2 * time.Second)

	snapshot, err := binance.GetDepthSnapshot(activeEnv.RestBaseURL, symbol, 1000)
	if err == nil {
		ob.InitWithSnapshot(snapshot)
	} else {
		log.Printf("[Main] ⚠️ 快照拉取失败 (测试网拥堵): %v", err)
	}

	// 5. 【核心】启动 UDS (Unix Domain Socket) HTTP 指令接收器
	// 🌟 增强版：UDS HTTP 服务的处理逻辑 (带极详尽的日志打印)
	http.HandleFunc("/api/order", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Symbol   string  `json:"symbol"`
			Side     string  `json:"side"`
			Quantity float64 `json:"quantity"`
			Price    float64 `json:"price"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			log.Printf("❌ [UDS 接收] 解析 Python 指令失败: %v", err)
			http.Error(w, "解析请求失败", http.StatusBadRequest)
			return
		}
		// 🚨 1. 新增：嚴格校驗與防呆攔截
		if req.Symbol == "" {
			log.Printf("❌ [嚴重錯誤] 拒絕發單！Python 傳來的 UDS 指令中 'symbol' 是空的！")
			http.Error(w, "symbol cannot be empty", http.StatusBadRequest)
			return
		}

		// 🚨 2. 修改：把 Python 傳來的真實 Symbol 也打印出來確認！
		log.Printf("🤖 [UDS 接收] 收到 Python 引擎指令: [%s] %s %.4f @ %.2f", req.Symbol, req.Side, req.Quantity, req.Price)

		startTime := time.Now()

		// 调用 API 客户端发起真实的交易请求
		respData, err := apiClient.PlaceOrder(req.Symbol, req.Side, "LIMIT", req.Quantity, req.Price)

		w.Header().Set("Content-Type", "application/json")

		// ==========================================
		// 🚨 核心修改：极详尽的失败与成功日志打印
		// ==========================================
		if err != nil {
			// 如果发单失败，极其醒目地打印币安返回的真实报错（例如 Insufficient Margin）
			log.Printf("❌ [执行失败] 极速发单被币安拒绝！耗时: %v", time.Since(startTime))
			log.Printf("⚠️ [错误详情]: %v", err)

			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"error": err.Error(),
			})
			return
		}

		// 如果发单成功，提取关键字段打印战报
		log.Printf("✅ [执行成功] 订单已发送至币安！耗时: %v", time.Since(startTime))

		// 容错提取返回值（防止某些字段为空导致 panic）
		orderId := respData["orderId"]
		status := respData["status"]
		avgPrice := respData["avgPrice"]

		log.Printf("📊 [订单回执] OrderID: %v | 状态: %v | 均价: %v", orderId, status, avgPrice)

		// 将完整的成功回执返回给 Python 引擎
		json.NewEncoder(w).Encode(respData)
	})

	go func() {
		sockFile := "/tmp/quant_engine.sock"
		_ = os.Remove(sockFile) // 启动前清理历史遗留的 sock 文件

		// 监听本地 Unix Socket，彻底绕过 TCP 端口
		listener, err := net.Listen("unix", sockFile)
		if err != nil {
			log.Fatalf("Socket listen error: %v", err)
		}

		// 限制 socket 文件权限为仅当前用户可读写，防止其他用户注入恶意订单
		if err := os.Chmod(sockFile, 0600); err != nil {
			log.Fatalf("Socket chmod error: %v", err)
		}

		log.Printf("[Main] 🎛️ 本地 UDS 极速通道已启动，监听文件: %s", sockFile)
		if err := http.Serve(listener, nil); err != nil {
			log.Fatalf("HTTP Serve error: %v", err)
		}
	}()

	// 6. 优雅退出机制
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	<-sigChan
	log.Println("\n[Main] 🛑 Shutdown signal received...")
	cancel()
	time.Sleep(1 * time.Second)
}
