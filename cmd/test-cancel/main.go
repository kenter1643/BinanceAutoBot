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
		log.Fatalf("读取配置失败: %v", err)
	}

	// 2. 智能路由
	activeEnv := cfg.Binance.GetActiveEnv()
	client := binance.NewAPIClient(activeEnv.APIKey, activeEnv.APISecret)
	client.BaseURL = activeEnv.RestBaseURL

	// 3. 构造撤单请求
	// 👇【请注意】把这里替换成你刚才那一单返回的真实 clientOrderId
	targetOrderID := "bot_test_1771811040774"

	cancelReq := binance.CancelOrderRequest{
		Symbol:            cfg.Binance.Symbol,
		OrigClientOrderID: targetOrderID,
	}

	log.Printf("🗑️ 准备撤销订单: Symbol=%s, ClientOrderID=%s", cancelReq.Symbol, cancelReq.OrigClientOrderID)

	// 4. 执行撤单调用
	resultJSON, err := client.CancelOrder(cancelReq)
	if err != nil {
		log.Fatalf("❌ 撤单失败: %v", err)
	}

	log.Printf("✅ 撤单成功！交易所返回结果:\n%s\n", resultJSON)
}
