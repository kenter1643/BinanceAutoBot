package binance

import (
	"context"
	"encoding/json"
	"log"
	"time" // 🌟 记得引入 time 包

	"github.com/gorilla/websocket"
)

// internal/binance/user_stream.go

// 🌟 终极防弹版 UserDataEvent 结构体
type UserDataEvent struct {
	EventType string `json:"e"` // 严格隔离：接收小写 e (字符串，例如 "ACCOUNT_UPDATE")
	EventTime int64  `json:"E"` // 严格隔离：吸收大写 E (数字时间戳，防止解析器崩溃)
	Account   struct {
		Balances []struct {
			Asset   string `json:"a"`
			Balance string `json:"wb"`
		} `json:"B"`
		Positions []struct {
			Symbol     string `json:"s"`
			Amount     string `json:"pa"`
			EntryPrice string `json:"ep"`
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
		// 内层循环：持续读取数据
		for {
			_, message, err := conn.ReadMessage()
			if err != nil {
				log.Printf("[UserStream] ⚠️ 私有通道异常断开: %v", err)
				break // 跳出内层循环，触发重连
			}

			// 🌟 1. 核心修复：先用一个通用的 map 解析，把所有事件的“真实面目”打印出来！
			var rawMsg map[string]interface{}
			if err := json.Unmarshal(message, &rawMsg); err != nil {
				log.Printf("[UserStream] ❌ 无法解析的原始 JSON: %s", string(message))
				continue
			}

			eventType, _ := rawMsg["e"].(string)

			// 🌟 2. 捕捉【资产与仓位更新】
			if eventType == "ACCOUNT_UPDATE" {
				log.Printf("📥 [UserStream] 收到资产更新 (ACCOUNT_UPDATE)")

				var event UserDataEvent
				if err := json.Unmarshal(message, &event); err == nil {
					onUpdate(event) // 将精确的结构体丢给 main.go 处理
				} else {
					// 如果解析失败，把红牌亮出来！
					log.Printf("❌ [UserStream] 结构体解析失败: %v | 原始数据: %s", err, string(message))
				}
			} else if eventType == "ORDER_TRADE_UPDATE" {
				// 🌟 3. 捕捉【订单成交状态更新】(极其重要，这是发单后最早回来的消息)
				orderData, ok := rawMsg["o"].(map[string]interface{})
				if ok {
					status, _ := orderData["X"].(string) // 订单当前状态 (NEW, FILLED, CANCELED)
					symbol, _ := orderData["s"].(string)
					log.Printf("🔔 [UserStream] 订单流转 -> [%s] 状态变为: %s", symbol, status)
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
