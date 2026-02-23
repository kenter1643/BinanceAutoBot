package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
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
		// 🌟 修改處 2：UserDataStream 推送更新 (約 100 行附近)
		go binance.StartUserDataStream(ctx, userDataWSURL, func(event binance.UserDataEvent) {
			for _, bal := range event.Account.Balances {
				if bal.Asset == "USDT" {
					_ = rdb.Set(ctx, "Wallet:USDT", bal.Balance, 0).Err()
				}
			}
			for _, pos := range event.Account.Positions {
				if pos.Symbol == symbol {
					_ = rdb.Set(ctx, "Position:"+symbol, pos.Amount, 0).Err()
					_ = rdb.Set(ctx, "EntryPrice:"+symbol, pos.EntryPrice, 0).Err() // 同步更新均價
					log.Printf("📦 [倉位更新] 持倉: %s | 均價: %s", pos.Amount, pos.EntryPrice)
				}
			}
		})
	}
	// ==========================================

	// 3. 启动行情状态机
	ob := orderbook.NewLocalOrderBook(symbol)
	wsClient := &binance.WSClient{
		URL: activeEnv.WSDepthURL,
		OnDepthFunc: func(event binance.WSDepthEvent) {
			_ = ob.ProcessDepthEvent(event)
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

	// 4. 异步 Redis 刷盘
	go func() {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		redisKey := "OrderBook:" + symbol
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if !ob.IsReady || !ob.Synced {
					continue
				}
				data, _ := json.Marshal(ob.GetTopN(20))
				_ = rdb.Set(ctx, redisKey, data, 0).Err()
			}
		}
	}()

	// 5. 【核心】启动 UDS (Unix Domain Socket) HTTP 指令接收器
	http.HandleFunc("/api/order", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Only POST allowed", http.StatusMethodNotAllowed)
			return
		}

		body, _ := io.ReadAll(r.Body)
		var cmd LocalCommandReq
		if err := json.Unmarshal(body, &cmd); err != nil {
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}

		log.Printf("🤖 [UDS 接收] 收到 Python 极速指令: %s %.4f @ %.2f", cmd.Side, cmd.Quantity, cmd.Price)

		// 组装并调用你的 API 客户端发单
		orderReq := binance.OrderRequest{
			Symbol:           symbol,
			Side:             cmd.Side,
			Type:             "LIMIT",
			Quantity:         cmd.Quantity,
			Price:            cmd.Price,
			TimeInForce:      "GTC",
			NewClientOrderID: fmt.Sprintf("bot_%d", time.Now().UnixMilli()),
		}

		// 这里调用的就是上面修复点初始化的 apiClient
		resultJSON, err := apiClient.PlaceOrder(orderReq)
		if err != nil {
			log.Printf("❌ [执行失败] %v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		log.Printf("✅ [执行成功] 极速订单已发送至币安！")
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(resultJSON))
	})

	go func() {
		sockFile := "/tmp/quant_engine.sock"
		_ = os.Remove(sockFile) // 启动前清理历史遗留的 sock 文件

		// 监听本地 Unix Socket，彻底绕过 TCP 端口
		listener, err := net.Listen("unix", sockFile)
		if err != nil {
			log.Fatalf("Socket listen error: %v", err)
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
