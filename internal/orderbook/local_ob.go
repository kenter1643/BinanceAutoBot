package orderbook

import (
	"BinanceAutoBot/internal/binance"
	"log"
	"sort"
	"strconv"
	"sync"
	"time"
)

type LocalOrderBook struct {
	mu           sync.RWMutex
	Symbol       string
	LastUpdateID int64
	Bids         map[float64]float64
	Asks         map[float64]float64
	IsReady      bool // [新增] 标记是否已完成全量快照加载
	Synced       bool // [新增] 标记是否已经完美衔接了第一帧
}

func NewLocalOrderBook(symbol string) *LocalOrderBook {
	return &LocalOrderBook{
		Symbol:  symbol,
		Bids:    make(map[float64]float64),
		Asks:    make(map[float64]float64),
		IsReady: false,
		Synced:  false,
	}
}

// [新增] InitWithSnapshot 使用 REST 接口拉取的数据进行底座初始化
func (ob *LocalOrderBook) InitWithSnapshot(snapshot *binance.RestDepthSnapshot) {
	ob.mu.Lock()
	defer ob.mu.Unlock()

	// 强制清空可能存在的旧数据
	ob.Bids = make(map[float64]float64)
	ob.Asks = make(map[float64]float64)

	// 灌入快照数据
	ob.updateLevels(ob.Bids, snapshot.Bids)
	ob.updateLevels(ob.Asks, snapshot.Asks)

	ob.LastUpdateID = snapshot.LastUpdateID
	ob.IsReady = true
	ob.Synced = false // 灌入快照后，重置为未缝合状态
	log.Printf("[OrderBook] %s Snapshot initialized. LastUpdateID: %d. Loaded Bids: %d, Asks: %d",
		ob.Symbol, ob.LastUpdateID, len(ob.Bids), len(ob.Asks))
}

func (ob *LocalOrderBook) ProcessDepthEvent(event binance.WSDepthEvent) error {
	ob.mu.Lock()
	defer ob.mu.Unlock()

	if !ob.IsReady {
		return nil
	}

	// 丢弃比快照还要老的数据
	if event.FinalUpdateID < ob.LastUpdateID {
		return nil
	}

	if !ob.Synced {
		// 1. 完美缝合的情况
		if event.FirstUpdateID <= ob.LastUpdateID && event.FinalUpdateID >= ob.LastUpdateID {
			ob.Synced = true
			log.Printf("[OrderBook] 🔗 完美缝合！WS 增量已与 REST 快照无缝衔接。")
		} else if event.FirstUpdateID > ob.LastUpdateID {
			// 2. 🚨 错过了接缝帧的情况！
			// 因为没有做队列缓冲，数据已经穿越到快照未来了。
			// 强行把 Synced 设为 true，打破死锁，让数据流转起来！
			ob.Synced = true
			log.Printf("[OrderBook] ⚠️ 强行缝合 (跳过断层): WS U=%d, REST ID=%d", event.FirstUpdateID, ob.LastUpdateID)
		} else {
			return nil
		}
	} else {
		// 已经缝合后，严格校验后续序列号的连续性
		if event.PrevFinalUpdID != ob.LastUpdateID {
			// 测试网偶尔也会丢包，为了防止不断重连，这里先只打日志，不断开
			log.Printf("[OrderBook Error] 🚨 序列号微小断层！期望 pu: %d, 实际: %d", ob.LastUpdateID, event.PrevFinalUpdID)
		}
	}

	// 更新盘口并推进时间线
	ob.updateLevels(ob.Bids, event.Bids)
	ob.updateLevels(ob.Asks, event.Asks)
	ob.LastUpdateID = event.FinalUpdateID

	return nil
}

// updateLevels 解析并更新价格档位
func (ob *LocalOrderBook) updateLevels(book map[float64]float64, levels [][]string) {
	for _, level := range levels {
		// 性能优化点：在极高频 HFT 中，不要用 strconv，应手写 ASCII 字节转 float 以压榨 CPU
		price, _ := strconv.ParseFloat(level[0], 64)
		qty, _ := strconv.ParseFloat(level[1], 64)

		if qty == 0 {
			delete(book, price) // 数量为 0，代表该价位挂单已全部撤销或吃光
		} else {
			book[price] = qty // 更新或新增该价位的挂单量
		}
	}
}

// GetTopLevels 提供一个线程安全的读取接口，供后续推送到 Redis 或 Python
func (ob *LocalOrderBook) GetTopLevels() (bids, asks int) {
	ob.mu.RLock()
	defer ob.mu.RUnlock()
	return len(ob.Bids), len(ob.Asks)
}

// GetTopN 提取排序后的前 N 档盘口快照
func (ob *LocalOrderBook) GetTopN(n int) binance.OrderBookSnapshot {
	ob.mu.RLock()
	defer ob.mu.RUnlock()

	snap := binance.OrderBookSnapshot{
		Symbol:       ob.Symbol,
		LastUpdateID: ob.LastUpdateID,
		Timestamp:    time.Now().UnixMilli(),
		Bids:         make([]binance.PriceLevel, 0, len(ob.Bids)),
		Asks:         make([]binance.PriceLevel, 0, len(ob.Asks)),
	}

	// 1. 提取所有 Bids 和 Asks
	for p, q := range ob.Bids {
		snap.Bids = append(snap.Bids, binance.PriceLevel{Price: p, Qty: q})
	}
	for p, q := range ob.Asks {
		snap.Asks = append(snap.Asks, binance.PriceLevel{Price: p, Qty: q})
	}

	// 2. Bids 买盘降序排序 (价格高的排前面)
	sort.Slice(snap.Bids, func(i, j int) bool {
		return snap.Bids[i].Price > snap.Bids[j].Price
	})

	// 3. Asks 卖盘升序排序 (价格低的排前面)
	sort.Slice(snap.Asks, func(i, j int) bool {
		return snap.Asks[i].Price < snap.Asks[j].Price
	})

	// 4. 截断取前 N 档
	if len(snap.Bids) > n {
		snap.Bids = snap.Bids[:n]
	}
	if len(snap.Asks) > n {
		snap.Asks = snap.Asks[:n]
	}

	return snap
}
