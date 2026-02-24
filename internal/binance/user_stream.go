package binance

import (
	"context"
	"encoding/json"
	"log"
	"time" // 🌟 记得引入 time 包

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
			Symbol     string `json:"s"`  // 交易对, 如 BTCUSDT
			Amount     string `json:"pa"` // 持仓量 (正数做多, 负数做空)
			EntryPrice string `json:"ep"` // 🌟 新增：開倉均價
		} `json:"P"`
	} `json:"a"`
}

// StartUserDataStream 启动私有 WebSocket 连接，并内置断线自动重连机制
func StartUserDataStream(ctx context.Context, wsURL string, onUpdate func(UserDataEvent)) {
	dialer := websocket.DefaultDialer
	backoff := 3 * time.Second
	const maxBackoff = 60 * time.Second

	// 外层循环：负责断线后的无限重连
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		log.Printf("[UserStream] 🔄 正在尝试连接私有资产频道...")
		conn, _, err := dialer.Dial(wsURL, nil)
		if err != nil {
			log.Printf("[UserStream] ❌ 连接失败: %v, %s后重试...", err, backoff)
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
				backoff *= 2
				if backoff > maxBackoff {
					backoff = maxBackoff
				}
			}
			continue
		}
		backoff = 3 * time.Second // 连接成功后重置退避时间

		log.Println("[UserStream] 🛡️ 账户私有资产监听通道已建立！等待资产变动...")

		// ==========================================
		// 💓 新增：WebSocket 底层 Ping 协程
		// 目的：每 60 秒主动发送一个 Ping 帧，防止被 AWS 负载均衡器因“长时间静默”踢下线
		// ==========================================
		pingTicker := time.NewTicker(60 * time.Second)
		pingDone := make(chan struct{})

		go func() {
			defer pingTicker.Stop()
			for {
				select {
				case <-pingDone:
					return // 连接断开时，安全退出这个保活协程
				case <-pingTicker.C:
					// 发送底层的 Ping 控制消息
					if err := conn.WriteControl(websocket.PingMessage, []byte{}, time.Now().Add(5*time.Second)); err != nil {
						return // 写入失败说明连接已断，退出协程
					}
				}
			}
		}()
		// ==========================================

		// 内层循环：持续读取数据
		for {
			_, message, err := conn.ReadMessage()
			if err != nil {
				log.Printf("[UserStream] ⚠️ 私有通道异常断开: %v", err)
				break // 跳出内层循环，触发重连
			}

			var event UserDataEvent
			if err := json.Unmarshal(message, &event); err == nil {
				if event.Event == "ACCOUNT_UPDATE" {
					onUpdate(event)
				}
			}
		}

		// 触发重连前的清理工作
		close(pingDone) // 停止当前连接的 Ping 协程
		conn.Close()    // 确保旧连接彻底关闭
		log.Println("[UserStream] ⏳ 准备进行断线重连...")
		time.Sleep(2 * time.Second)
	}
}
