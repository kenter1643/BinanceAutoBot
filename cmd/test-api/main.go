// cmd/test-api/main.go

package main

import (
	"log"

	"BinanceAutoBot2/internal/binance"
	"BinanceAutoBot2/internal/config"
)

func main() {
	// 1. 加载配置
	cfg, err := config.LoadConfig("config.json")
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// 2. 智能获取当前激活的环境配置 (一键切换的核心)
	activeEnv := cfg.Binance.GetActiveEnv()
	log.Printf("Current Active Environment: [%s]", cfg.Binance.ActiveEnv)

	if activeEnv.APIKey == "" {
		log.Fatal("Error: API Key is empty for the active environment.")
	}

	// 3. 实例化 API 客户端
	client := binance.NewAPIClient(activeEnv.APIKey, activeEnv.APISecret)
	client.BaseURL = activeEnv.RestBaseURL

	// 4. 发起余额查询测试
	log.Printf("Fetching Balance from: %s", client.BaseURL)
	balanceJSON, err := client.GetAccountBalance()
	if err != nil {
		log.Fatalf("Failed to get balance: %v", err)
	}

	log.Println("Account Balance Response:", balanceJSON)
}
