package integration

import (
	"BinanceAutoBot2/internal/binance"
	"BinanceAutoBot2/internal/orderbook"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

// ---- 辅助函数 ----

func redisAvailable() bool {
	rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:6379"})
	defer rdb.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	return rdb.Ping(ctx).Err() == nil
}

// ---- OrderBook → Redis 集成测试 ----

func TestOrderBookToRedis(t *testing.T) {
	if !redisAvailable() {
		t.Skip("Redis not available, skipping integration test")
	}

	rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:6379", DB: 15}) // 用 DB15 隔离测试数据
	defer rdb.FlushDB(context.Background())
	defer rdb.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ob := orderbook.NewLocalOrderBook("BTCUSDT")
	ob.InitWithSnapshot(&binance.RestDepthSnapshot{
		LastUpdateID: 100,
		Bids:         [][]string{{"50000", "1.5"}, {"49999", "2.0"}},
		Asks:         [][]string{{"50001", "1.0"}, {"50002", "0.5"}},
	})
	ob.ProcessDepthEvent(binance.WSDepthEvent{
		FirstUpdateID:  99,
		FinalUpdateID:  101,
		PrevFinalUpdID: 98,
	})

	data, err := json.Marshal(ob.GetTopN(20))
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	if err := rdb.Set(ctx, "OrderBook:BTCUSDT", data, 0).Err(); err != nil {
		t.Fatalf("redis set failed: %v", err)
	}

	raw, err := rdb.Get(ctx, "OrderBook:BTCUSDT").Result()
	if err != nil {
		t.Fatalf("redis get failed: %v", err)
	}

	var snap binance.OrderBookSnapshot
	if err := json.Unmarshal([]byte(raw), &snap); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if snap.Symbol != "BTCUSDT" {
		t.Errorf("expected BTCUSDT, got %s", snap.Symbol)
	}
	if len(snap.Bids) == 0 || len(snap.Asks) == 0 {
		t.Error("expected non-empty bids and asks")
	}
	// 验证 bids 降序
	if len(snap.Bids) > 1 && snap.Bids[0].Price < snap.Bids[1].Price {
		t.Error("bids should be sorted descending")
	}
	// 验证 asks 升序
	if len(snap.Asks) > 1 && snap.Asks[0].Price > snap.Asks[1].Price {
		t.Error("asks should be sorted ascending")
	}
}

func TestRedisPositionAndBalance(t *testing.T) {
	if !redisAvailable() {
		t.Skip("Redis not available, skipping integration test")
	}

	rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:6379", DB: 15})
	defer rdb.FlushDB(context.Background())
	defer rdb.Close()

	ctx := context.Background()

	_ = rdb.Set(ctx, "Wallet:USDT", "1234.56", 0).Err()
	_ = rdb.Set(ctx, "Position:BTCUSDT", "0.01", 0).Err()
	_ = rdb.Set(ctx, "EntryPrice:BTCUSDT", "50000.0", 0).Err()

	balance, _ := rdb.Get(ctx, "Wallet:USDT").Result()
	pos, _ := rdb.Get(ctx, "Position:BTCUSDT").Result()
	ep, _ := rdb.Get(ctx, "EntryPrice:BTCUSDT").Result()

	if balance != "1234.56" {
		t.Errorf("expected 1234.56, got %s", balance)
	}
	if pos != "0.01" {
		t.Errorf("expected 0.01, got %s", pos)
	}
	if ep != "50000.0" {
		t.Errorf("expected 50000.0, got %s", ep)
	}
}

// ---- UDS HTTP 集成测试 ----

func TestUDSOrderEndpoint(t *testing.T) {
	sockFile := "/tmp/test_quant_integration.sock"
	_ = os.Remove(sockFile)

	mux := http.NewServeMux()
	received := make(chan string, 1)
	mux.HandleFunc("/api/order", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "only POST", http.StatusMethodNotAllowed)
			return
		}
		var body map[string]interface{}
		json.NewDecoder(r.Body).Decode(&body)
		data, _ := json.Marshal(body)
		received <- string(data)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"clientOrderId": "test_order_001"})
	})

	listener, err := net.Listen("unix", sockFile)
	if err != nil {
		t.Fatalf("failed to listen on UDS: %v", err)
	}
	defer os.Remove(sockFile)
	defer listener.Close()

	srv := &http.Server{Handler: mux}
	go srv.Serve(listener)
	defer srv.Close()

	// 用标准 http.Client + DialContext 连接 UDS
	client := &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, "unix", sockFile)
			},
		},
		Timeout: 2 * time.Second,
	}

	payload := `{"side":"BUY","quantity":0.01,"price":50000.0}`
	resp, err := client.Post("http://unix/api/order", "application/json",
		strings.NewReader(payload))
	if err != nil {
		t.Fatalf("UDS request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	select {
	case body := <-received:
		var m map[string]interface{}
		json.Unmarshal([]byte(body), &m)
		if m["side"] != "BUY" {
			t.Errorf("expected side=BUY, got %v", m["side"])
		}
		if m["quantity"].(float64) != 0.01 {
			t.Errorf("expected quantity=0.01, got %v", m["quantity"])
		}
	case <-time.After(2 * time.Second):
		t.Error("timeout waiting for order to be received")
	}
}

func TestUDSOrderEndpoint_RejectNonPost(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Only POST allowed", http.StatusMethodNotAllowed)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/order")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", resp.StatusCode)
	}
}

// ---- APIClient + mock server 集成测试 ----

func TestAPIClientFullOrderFlow(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/fapi/v1/listenKey":
			if r.Method == http.MethodPost {
				json.NewEncoder(w).Encode(map[string]string{"listenKey": "integration_key"})
			} else if r.Method == http.MethodPut {
				w.WriteHeader(http.StatusOK)
			}
		case "/fapi/v1/order":
			if r.Method == http.MethodPost {
				json.NewEncoder(w).Encode(map[string]interface{}{
					"orderId":       99999,
					"clientOrderId": "bot_integration",
					"status":        "NEW",
				})
			} else if r.Method == http.MethodDelete {
				json.NewEncoder(w).Encode(map[string]interface{}{
					"orderId": 99999,
					"status":  "CANCELED",
				})
			}
		case "/fapi/v2/positionRisk":
			json.NewEncoder(w).Encode([]map[string]interface{}{
				{"positionAmt": "0.01", "entryPrice": "50000.0"},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c := binance.NewAPIClient("test_key", "test_secret")
	c.BaseURL = srv.URL

	// 1. 获取 ListenKey
	key, err := c.GetListenKey()
	if err != nil || key != "integration_key" {
		t.Fatalf("GetListenKey failed: %v, key=%s", err, key)
	}

	// 2. 续期 ListenKey
	if err := c.RenewListenKey(key); err != nil {
		t.Fatalf("RenewListenKey failed: %v", err)
	}

	// 3. 下单
	result, err := c.PlaceOrder(binance.OrderRequest{
		Symbol:           "BTCUSDT",
		Side:             "BUY",
		Type:             "LIMIT",
		Quantity:         0.01,
		Price:            50000.0,
		TimeInForce:      "GTC",
		NewClientOrderID: "bot_integration",
	})
	if err != nil {
		t.Fatalf("PlaceOrder failed: %v", err)
	}
	if result == "" {
		t.Error("PlaceOrder result should not be empty")
	}

	// 4. 查询仓位
	amt, ep, err := c.GetPosition("BTCUSDT")
	if err != nil || amt != "0.01" || ep != "50000.0" {
		t.Fatalf("GetPosition failed: %v, amt=%s, ep=%s", err, amt, ep)
	}

	// 5. 撤单
	cancelResult, err := c.CancelOrder(binance.CancelOrderRequest{
		Symbol:            "BTCUSDT",
		OrigClientOrderID: "bot_integration",
	})
	if err != nil {
		t.Fatalf("CancelOrder failed: %v", err)
	}
	if cancelResult == "" {
		t.Error("CancelOrder result should not be empty")
	}
}
