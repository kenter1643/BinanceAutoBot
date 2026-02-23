package binance

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"github.com/gorilla/websocket"
)

// WSClient 带有自动重连机制的 WebSocket 客户端
type WSClient struct {
	URL         string
	OnDepthFunc func(event WSDepthEvent) // 回调函数：将网络层与业务层解耦
}

// Start 启动客户端并阻塞运行，直到 ctx 被取消
func (c *WSClient) Start(ctx context.Context) {
	for {
		err := c.connectAndRead(ctx)
		if err != nil {
			log.Printf("[WS Client] Connection error: %v. Reconnecting in 2 seconds...", err)
		}

		// 检查是否是因为上下文取消（系统退出）而断开的
		select {
		case <-ctx.Done():
			log.Println("[WS Client] Context canceled, exiting reconnect loop.")
			return
		case <-time.After(2 * time.Second): // 简单的固定延迟重连 (实盘建议用指数退避算法)
			continue
		}
	}
}

func (c *WSClient) connectAndRead(ctx context.Context) error {
	log.Printf("[WS Client] Dialing %s", c.URL)
	conn, _, err := websocket.DefaultDialer.Dial(c.URL, nil)
	if err != nil {
		return err
	}
	defer conn.Close()

	// 开启一个 Goroutine 监听 ctx 的取消信号，以便优雅关闭连接
	go func() {
		<-ctx.Done()
		conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "System shutting down"))
	}()

	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			return err // 读取失败，返回 err 触发外部的重连机制
		}

		var event WSDepthEvent
		// 性能优化点：实盘中可替换为 github.com/goccy/go-json 提升解析速度
		if err := json.Unmarshal(message, &event); err != nil {
			log.Printf("[WS Client] JSON parse error: %v", err)
			// 👇 新增下面这一行，把原始的 byte 数组转成 string 打印出来
			log.Printf("[WS Client] Raw payload: %s", string(message))
			continue
		}

		// 通过回调函数将数据推给 OrderBook，网络层不关心业务逻辑
		if c.OnDepthFunc != nil {
			c.OnDepthFunc(event)
		}
	}
}
