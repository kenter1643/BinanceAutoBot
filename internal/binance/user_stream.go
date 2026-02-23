package binance

import (
	"context"
	"encoding/json"
	"log"

	"github.com/gorilla/websocket"
)

// UserDataEvent 定义极其精简的私有推送事件结构 (过滤掉无关的冗余字段)
type UserDataEvent struct {
	Event   string `json:"e"` // 事件类型, 例如 "ACCOUNT_UPDATE"
	Account struct {
		Balances []struct {
			Asset   string `json:"a"`  // 资产名, 如 USDT
			Balance string `json:"wb"` // 钱包余额 (Wallet Balance)
		} `json:"B"`
		Positions []struct {
			Symbol string `json:"s"`  // 交易对, 如 BTCUSDT
			Amount string `json:"pa"` // 持仓量 (正数做多, 负数做空)
		} `json:"P"`
	} `json:"a"`
}

// StartUserDataStream 启动独立的私有 WebSocket 连接
func StartUserDataStream(ctx context.Context, wsURL string, onUpdate func(UserDataEvent)) {
	dialer := websocket.DefaultDialer
	conn, _, err := dialer.Dial(wsURL, nil)
	if err != nil {
		log.Printf("[UserStream] ❌ 连接私有频道失败: %v", err)
		return
	}
	defer conn.Close()

	log.Println("[UserStream] 🛡️ 账户私有资产监听通道已建立！等待资产变动...")

	for {
		select {
		case <-ctx.Done():
			return
		default:
			_, message, err := conn.ReadMessage()
			if err != nil {
				log.Printf("[UserStream] ⚠️ 私有通道断开: %v", err)
				return
			}

			var event UserDataEvent
			if err := json.Unmarshal(message, &event); err == nil {
				// 我们目前只关心账户余额和仓位的变动
				if event.Event == "ACCOUNT_UPDATE" {
					onUpdate(event)
				}
			}
		}
	}
}
