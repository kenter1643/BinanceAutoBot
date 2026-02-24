package binance

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// ---- createSignature ----

func TestCreateSignature(t *testing.T) {
	c := &APIClient{APISecret: "mysecret"}
	sig := c.createSignature("symbol=BTCUSDT&timestamp=1234567890")
	if sig == "" {
		t.Error("signature should not be empty")
	}
	// 相同输入应产生相同签名
	sig2 := c.createSignature("symbol=BTCUSDT&timestamp=1234567890")
	if sig != sig2 {
		t.Error("same input should produce same signature")
	}
	// 不同输入应产生不同签名
	sig3 := c.createSignature("symbol=BTCUSDT&timestamp=9999999999")
	if sig == sig3 {
		t.Error("different input should produce different signature")
	}
}

// ---- GetListenKey ----

func TestGetListenKey_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.Header.Get("X-MBX-APIKEY") != "test_api_key" {
			t.Errorf("missing or wrong API key header")
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"listenKey": "abc123"})
	}))
	defer srv.Close()

	c := NewAPIClient("test_api_key", "test_secret")
	c.BaseURL = srv.URL

	key, err := c.GetListenKey()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if key != "abc123" {
		t.Errorf("expected abc123, got %s", key)
	}
}

func TestGetListenKey_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"code":-2014,"msg":"API-key format invalid."}`))
	}))
	defer srv.Close()

	c := NewAPIClient("bad_key", "bad_secret")
	c.BaseURL = srv.URL

	_, err := c.GetListenKey()
	if err == nil {
		t.Error("expected error for 401 response")
	}
}

// ---- RenewListenKey ----

func TestRenewListenKey_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("expected PUT, got %s", r.Method)
		}
		if r.URL.Query().Get("listenKey") != "mykey" {
			t.Errorf("expected listenKey=mykey in query")
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := NewAPIClient("key", "secret")
	c.BaseURL = srv.URL

	if err := c.RenewListenKey("mykey"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRenewListenKey_Failure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"code":-1100,"msg":"Illegal characters found in parameter"}`))
	}))
	defer srv.Close()

	c := NewAPIClient("key", "secret")
	c.BaseURL = srv.URL

	if err := c.RenewListenKey("bad_key"); err == nil {
		t.Error("expected error for 400 response")
	}
}

// ---- PlaceOrder ----

func TestPlaceOrder_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"orderId":       12345,
			"clientOrderId": "bot_test",
			"status":        "NEW",
		})
	}))
	defer srv.Close()

	c := NewAPIClient("key", "secret")
	c.BaseURL = srv.URL

	result, err := c.PlaceOrder(OrderRequest{
		Symbol:           "BTCUSDT",
		Side:             "BUY",
		Type:             "LIMIT",
		Quantity:         0.01,
		Price:            50000.0,
		TimeInForce:      "GTC",
		NewClientOrderID: "bot_test",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == "" {
		t.Error("result should not be empty")
	}
}

func TestPlaceOrder_InsufficientBalance(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"code":-2019,"msg":"Margin is insufficient."}`))
	}))
	defer srv.Close()

	c := NewAPIClient("key", "secret")
	c.BaseURL = srv.URL

	_, err := c.PlaceOrder(OrderRequest{Symbol: "BTCUSDT", Side: "BUY", Type: "MARKET", Quantity: 999})
	if err == nil {
		t.Error("expected error for insufficient balance")
	}
}

// ---- GetPosition ----

func TestGetPosition_WithPosition(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode([]map[string]interface{}{
			{"positionAmt": "0.01", "entryPrice": "50000.0"},
		})
	}))
	defer srv.Close()

	c := NewAPIClient("key", "secret")
	c.BaseURL = srv.URL

	amt, ep, err := c.GetPosition("BTCUSDT")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if amt != "0.01" {
		t.Errorf("expected 0.01, got %s", amt)
	}
	if ep != "50000.0" {
		t.Errorf("expected 50000.0, got %s", ep)
	}
}

func TestGetPosition_Empty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode([]map[string]interface{}{})
	}))
	defer srv.Close()

	c := NewAPIClient("key", "secret")
	c.BaseURL = srv.URL

	amt, ep, err := c.GetPosition("BTCUSDT")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if amt != "0.0" || ep != "0.0" {
		t.Errorf("expected 0.0/0.0 for empty position, got %s/%s", amt, ep)
	}
}

// ---- NewAPIClient ----

func TestNewAPIClient(t *testing.T) {
	c := NewAPIClient("mykey", "mysecret")
	if c.APIKey != "mykey" {
		t.Errorf("expected mykey, got %s", c.APIKey)
	}
	if c.APISecret != "mysecret" {
		t.Errorf("expected mysecret, got %s", c.APISecret)
	}
	if c.HTTPClient.Timeout != 5*time.Second {
		t.Errorf("expected 5s timeout, got %v", c.HTTPClient.Timeout)
	}
}
