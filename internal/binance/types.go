package binance

// WSDepthEvent 对应币安 U本位合约增量深度推送的 JSON 结构
type WSDepthEvent struct {
	EventType       string     `json:"e"` // [新增避雷针] 专门吸收 "e" ("depthUpdate")，防止 Go 乱匹配
	EventTime       int64      `json:"E"` // 恢复为 int64，接收事件时间
	TransactionTime int64      `json:"T"` // 顺手把撮合时间也加上，后续做延迟监控会用到
	Symbol          string     `json:"s"`
	FirstUpdateID   int64      `json:"U"`
	FinalUpdateID   int64      `json:"u"`
	PrevFinalUpdID  int64      `json:"pu"`
	Bids            [][]string `json:"b"`
	Asks            [][]string `json:"a"`
}

// RestDepthSnapshot 对应币安 U本位合约 REST API 返回的全量深度快照
type RestDepthSnapshot struct {
	LastUpdateID int64      `json:"lastUpdateId"`
	Bids         [][]string `json:"bids"`
	Asks         [][]string `json:"asks"`
}

// PriceLevel 极简的价格档位，省带宽
type PriceLevel struct {
	Price float64 `json:"p"`
	Qty   float64 `json:"q"`
}

// OrderBookSnapshot 供策略端读取的最终标准切片
type OrderBookSnapshot struct {
	Symbol       string       `json:"s"`
	LastUpdateID int64        `json:"u"` // 策略端可以通过校验这个ID，判断切片是否变动
	Timestamp    int64        `json:"t"` // Redis 写入时间戳
	Bids         []PriceLevel `json:"b"` // 买盘 (价格从高到低排序)
	Asks         []PriceLevel `json:"a"` // 卖盘 (价格从低到高排序)
}
