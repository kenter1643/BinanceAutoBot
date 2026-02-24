package orderbook

import (
	"BinanceAutoBot2/internal/binance"
	"sync"
	"testing"
)

func makeSnapshot(lastUpdateID int64, bids, asks [][]string) *binance.RestDepthSnapshot {
	return &binance.RestDepthSnapshot{
		LastUpdateID: lastUpdateID,
		Bids:         bids,
		Asks:         asks,
	}
}

func makeEvent(firstID, finalID, prevID int64, bids, asks [][]string) binance.WSDepthEvent {
	return binance.WSDepthEvent{
		FirstUpdateID:  firstID,
		FinalUpdateID:  finalID,
		PrevFinalUpdID: prevID,
		Bids:           bids,
		Asks:           asks,
	}
}

func TestNewLocalOrderBook(t *testing.T) {
	ob := NewLocalOrderBook("BTCUSDT")
	if ob.Symbol != "BTCUSDT" {
		t.Errorf("expected BTCUSDT, got %s", ob.Symbol)
	}
	if ob.IsReady || ob.Synced {
		t.Error("new orderbook should not be ready or synced")
	}
}

func TestInitWithSnapshot(t *testing.T) {
	ob := NewLocalOrderBook("BTCUSDT")
	snap := makeSnapshot(100, [][]string{{"50000", "1.5"}}, [][]string{{"50001", "2.0"}})
	ob.InitWithSnapshot(snap)

	if !ob.IsReady {
		t.Error("should be ready after snapshot")
	}
	if ob.Synced {
		t.Error("should not be synced yet after snapshot")
	}
	if ob.LastUpdateID != 100 {
		t.Errorf("expected LastUpdateID=100, got %d", ob.LastUpdateID)
	}
	bids, asks := ob.GetTopLevels()
	if bids != 1 || asks != 1 {
		t.Errorf("expected 1 bid and 1 ask, got %d/%d", bids, asks)
	}
}

func TestProcessDepthEvent_PerfectSync(t *testing.T) {
	ob := NewLocalOrderBook("BTCUSDT")
	ob.InitWithSnapshot(makeSnapshot(100, [][]string{{"50000", "1.0"}}, [][]string{{"50001", "1.0"}}))

	// 完美缝合：FirstUpdateID <= 100 <= FinalUpdateID
	err := ob.ProcessDepthEvent(makeEvent(99, 101, 98, [][]string{{"50000", "2.0"}}, nil))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ob.Synced {
		t.Error("should be synced after perfect stitch")
	}
	if ob.LastUpdateID != 101 {
		t.Errorf("expected LastUpdateID=101, got %d", ob.LastUpdateID)
	}
}

func TestProcessDepthEvent_DiscardOldEvent(t *testing.T) {
	ob := NewLocalOrderBook("BTCUSDT")
	ob.InitWithSnapshot(makeSnapshot(100, nil, nil))

	// FinalUpdateID < LastUpdateID，应丢弃
	err := ob.ProcessDepthEvent(makeEvent(90, 95, 89, [][]string{{"50000", "9.9"}}, nil))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ob.Synced {
		t.Error("should not be synced after discarded event")
	}
}

func TestProcessDepthEvent_ForceSync(t *testing.T) {
	ob := NewLocalOrderBook("BTCUSDT")
	ob.InitWithSnapshot(makeSnapshot(100, nil, nil))

	// FirstUpdateID > LastUpdateID，强行缝合
	err := ob.ProcessDepthEvent(makeEvent(105, 110, 104, nil, nil))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ob.Synced {
		t.Error("should be force-synced")
	}
}

func TestProcessDepthEvent_SequenceGapTriggersResync(t *testing.T) {
	ob := NewLocalOrderBook("BTCUSDT")
	ob.InitWithSnapshot(makeSnapshot(100, nil, nil))

	// 先完美缝合
	ob.ProcessDepthEvent(makeEvent(99, 101, 98, nil, nil))

	// 序列号断层：期望 pu=101，实际传入 pu=999
	ob.ProcessDepthEvent(makeEvent(1000, 1001, 999, nil, nil))

	if ob.IsReady {
		t.Error("should not be ready after sequence gap")
	}
	if !ob.CheckAndClearResync() {
		t.Error("NeedsResync should be true after sequence gap")
	}
	// CheckAndClearResync 调用后应清除标志
	if ob.CheckAndClearResync() {
		t.Error("NeedsResync should be cleared after CheckAndClearResync")
	}
}

func TestProcessDepthEvent_NotReady(t *testing.T) {
	ob := NewLocalOrderBook("BTCUSDT")
	// 未初始化快照，直接处理事件应静默忽略
	err := ob.ProcessDepthEvent(makeEvent(1, 2, 0, nil, nil))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestUpdateLevels_DeleteZeroQty(t *testing.T) {
	ob := NewLocalOrderBook("BTCUSDT")
	ob.InitWithSnapshot(makeSnapshot(100, [][]string{{"50000", "1.0"}, {"49999", "2.0"}}, nil))

	// 数量为 0 应删除该档位
	ob.ProcessDepthEvent(makeEvent(99, 101, 98, [][]string{{"50000", "0"}}, nil))

	bids, _ := ob.GetTopLevels()
	if bids != 1 {
		t.Errorf("expected 1 bid after deletion, got %d", bids)
	}
}

func TestGetTopN_Sorting(t *testing.T) {
	ob := NewLocalOrderBook("BTCUSDT")
	ob.InitWithSnapshot(makeSnapshot(100,
		[][]string{{"49998", "1"}, {"50000", "1"}, {"49999", "1"}},
		[][]string{{"50002", "1"}, {"50001", "1"}, {"50003", "1"}},
	))
	ob.ProcessDepthEvent(makeEvent(99, 101, 98, nil, nil))

	snap := ob.GetTopN(3)

	// Bids 应降序
	if snap.Bids[0].Price < snap.Bids[1].Price {
		t.Error("bids should be sorted descending")
	}
	// Asks 应升序
	if snap.Asks[0].Price > snap.Asks[1].Price {
		t.Error("asks should be sorted ascending")
	}
}

func TestGetTopN_Truncation(t *testing.T) {
	ob := NewLocalOrderBook("BTCUSDT")
	ob.InitWithSnapshot(makeSnapshot(100,
		[][]string{{"1", "1"}, {"2", "1"}, {"3", "1"}, {"4", "1"}, {"5", "1"}},
		[][]string{{"6", "1"}, {"7", "1"}, {"8", "1"}, {"9", "1"}, {"10", "1"}},
	))
	ob.ProcessDepthEvent(makeEvent(99, 101, 98, nil, nil))

	snap := ob.GetTopN(3)
	if len(snap.Bids) != 3 {
		t.Errorf("expected 3 bids, got %d", len(snap.Bids))
	}
	if len(snap.Asks) != 3 {
		t.Errorf("expected 3 asks, got %d", len(snap.Asks))
	}
}

func TestConcurrentAccess(t *testing.T) {
	ob := NewLocalOrderBook("BTCUSDT")
	ob.InitWithSnapshot(makeSnapshot(100,
		[][]string{{"50000", "1.0"}},
		[][]string{{"50001", "1.0"}},
	))
	ob.ProcessDepthEvent(makeEvent(99, 101, 98, nil, nil))

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			ob.GetTopN(5)
		}()
		go func(id int64) {
			defer wg.Done()
			ob.ProcessDepthEvent(makeEvent(id, id+1, id-1, [][]string{{"50000", "1.0"}}, nil))
		}(int64(101 + i))
	}
	wg.Wait()
}
