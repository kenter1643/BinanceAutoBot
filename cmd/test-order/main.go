package main

import (
	"fmt"
	"log"
	"time"

	"BinanceAutoBot2/internal/binance"
	"BinanceAutoBot2/internal/config"
)

func main() {
	// 1. 載入配置
	cfg, err := config.LoadConfig("config.json")
	if err != nil {
		log.Fatalf("讀取配置失敗: %v", err)
	}

	// 2. 智慧路由與安全防護
	activeEnv := cfg.Binance.GetActiveEnv()
	if cfg.Binance.ActiveEnv == "mainnet" {
		log.Println("⚠️ 警告：目前處於【主網】環境！這筆訂單將會動用真實資金！")
		// 在實盤測試時，可以加上 time.Sleep(5 * time.Second) 給自己留個後悔藥時間
	} else {
		log.Println("✅ 目前處於【測試網】環境，準備發送模擬訂單...")
	}

	client := binance.NewAPIClient(activeEnv.APIKey, activeEnv.APISecret)
	client.BaseURL = activeEnv.RestBaseURL

	// 3. 構造訂單請求 (以 BTCUSDT 為例)
	// 這裡示範：掛一筆在 30000 USDT 的限價買單，數量 0.001 BTC
	orderReq := binance.OrderRequest{
		Symbol:      cfg.Binance.Symbol,
		Side:        "BUY",
		Type:        "LIMIT",
		Quantity:    0.01,
		Price:       68950.00,
		TimeInForce: "GTC", // GTC: 一直有效直到取消
		// [量化核心] 動態生成唯一訂單號，包含時間戳，防重發且易於日誌追蹤
		NewClientOrderID: fmt.Sprintf("bot_test_%d", time.Now().UnixMilli()),
	}

	log.Printf("🚀 準備送出訂單: [%s] %s %f 顆, 掛單價: %f",
		orderReq.Symbol, orderReq.Side, orderReq.Quantity, orderReq.Price)

	// 4. 執行發單呼叫
	orderJSON, err := client.PlaceOrder(orderReq)
	if err != nil {
		log.Fatalf("❌ 發單失敗: %v", err)
	}

	log.Printf("✅ 發單成功！交易所回傳結果:\n%s\n", orderJSON)
}
