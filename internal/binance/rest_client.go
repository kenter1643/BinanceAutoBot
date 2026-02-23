package binance

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// GetDepthSnapshot 通过 REST API 拉取指定深度的全量快照
// limit 可选值: 5, 10, 20, 50, 100, 500, 1000
// "u": 9964460657741：这是币安的 UpdateID，你的 Python 策略每次读取时，只要对比这个 ID 是否变化，就能知道盘口有没有更新，避免重复计算。
// "t": 1771805373487：这是 Go 写入 Redis 的绝对毫秒时间戳。你可以用它来监控“数据新鲜度”。
// 严格排序的买卖盘：你看 b (Bids买盘) 是从 67634.9 严格递减的，而 a (Asks卖盘) 是从 67635 严格递增的。买一和卖一之间的点差 (Spread) 只有 0.1 USDT。
// GetDepthSnapshot 增加 baseURL 参数，实现环境隔离
func GetDepthSnapshot(baseURL, symbol string, limit int) (*RestDepthSnapshot, error) {
	// 动态拼接环境 URL
	url := fmt.Sprintf("%s/fapi/v1/depth?symbol=%s&limit=%d", baseURL, symbol, limit)

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var snapshot RestDepthSnapshot
	if err := json.NewDecoder(resp.Body).Decode(&snapshot); err != nil {
		return nil, err
	}
	return &snapshot, nil
}
