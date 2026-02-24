// cmd/test-order/main.go
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"time"

	"BinanceAutoBot2/internal/config"

	"github.com/redis/go-redis/v9"
)

// OrderBookData 对应 Go 网关写入 Redis 的盘口结构
// 🌟 修复：将 Price 和 Qty 的类型从 string 改为 float64
type OrderBookData struct {
	Bids []struct {
		Price float64 `json:"p"`
		Qty   float64 `json:"q"`
	} `json:"b"`
	Asks []struct {
		Price float64 `json:"p"`
		Qty   float64 `json:"q"`
	} `json:"a"`
}

func main() {
	// 1. 加载统一配置
	cfg, err := config.LoadConfig("config.json")
	if err != nil {
		log.Fatalf("❌ 读取配置失败: %v", err)
	}
	symbol := cfg.Binance.Symbol

	// 2. 初始化 Redis 连接
	rdb := redis.NewClient(&redis.Options{Addr: cfg.Redis.Addr, DB: cfg.Redis.DB})
	ctx := context.Background()
	if err := rdb.Ping(ctx).Err(); err != nil {
		log.Fatalf("❌ Redis 连接失败: %v", err)
	}
	log.Println("✅ Redis 连接成功，准备拉取最新盘口...")

	// 3. 从 Redis 极速读取最新盘口切片
	redisKey := "OrderBook:" + symbol
	obJSON, err := rdb.Get(ctx, redisKey).Result()
	if err != nil {
		log.Fatalf("❌ 从 Redis 读取盘口失败: %v", err)
	}

	var ob OrderBookData
	if err := json.Unmarshal([]byte(obJSON), &ob); err != nil {
		log.Fatalf("❌ 盘口 JSON 解析失败: %v", err)
	}

	if len(ob.Asks) == 0 || len(ob.Bids) == 0 {
		log.Fatalf("❌ 盘口数据为空")
	}

	// 4. 提取最新价格并计算目标开火价
	// 🌟 修复：既然已经是 float64，就不需要 strconv.ParseFloat 转换了，直接拿来算！
	bestAsk := ob.Asks[0].Price
	targetPrice := bestAsk + 5.0

	// 保留两位小数用于日志打印
	targetPriceStr := fmt.Sprintf("%.2f", targetPrice)

	log.Printf("📊 当前 [%s] 真实卖一价: %.2f", symbol, bestAsk)
	log.Printf("🎯 决定使用激进吃单价: %s", targetPriceStr)

	// 5. 组装发单指令
	orderReq := map[string]interface{}{
		"symbol":   symbol,
		"side":     "BUY",
		"quantity": 0.01,
		"price":    targetPrice,
	}
	reqBody, _ := json.Marshal(orderReq)

	// 6. 构建 UDS (Unix Domain Socket) HTTP 客户端
	udsClient := &http.Client{
		Transport: &http.Transport{
			DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
				return net.Dial("unix", "/tmp/quant_engine.sock")
			},
		},
		Timeout: 10 * time.Second,
	}

	log.Printf("🚀 正在通过底层 UDS 管道发送下单指令...")

	resp, err := udsClient.Post("http://unix/api/order", "application/json", bytes.NewReader(reqBody))
	if err != nil {
		log.Fatalf("❌ UDS 发单请求失败: %v", err)
	}
	defer resp.Body.Close()

	// 7. 解析网关返回的执行结果
	var respData map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&respData)

	if resp.StatusCode == 200 {
		log.Printf("✅ UDS 极速下单测试成功! 网关返回: %v", respData)
	} else {
		log.Printf("⚠️ 下单被拒! 状态码: %d, 返回: %v", resp.StatusCode, respData)
	}
}
