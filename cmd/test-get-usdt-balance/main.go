package main

import (
	"log"

	"BinanceAutoBot2/internal/binance"
	"BinanceAutoBot2/internal/config"
)

func main() {
	// 1. 加载统一配置
	cfg, err := config.LoadConfig("config.json")
	if err != nil {
		log.Fatalf("❌ 读取配置失败: %v", err)
	}

	activeEnv := cfg.Binance.GetActiveEnv()

	// 2. 初始化 API 客户端
	apiClient := binance.NewAPIClient(activeEnv.APIKey, activeEnv.APISecret)
	apiClient.BaseURL = activeEnv.RestBaseURL

	log.Printf("🔍 正在查询 [%s] 环境的账户余额...", cfg.Binance.ActiveEnv)

	// 3. 调用我们刚刚写好的主动查询接口
	balance, err := apiClient.GetUSDTBalance()
	if err != nil {
		log.Fatalf("❌ 查询失败: %v", err)
	}

	log.Printf("✅ 查询成功！")
	log.Printf("💰 当前可用 USDT 余额: %s", balance)
}
