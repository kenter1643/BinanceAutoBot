package binance

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// APIClient 负责处理所有带鉴权的私有 API 请求
type APIClient struct {
	BaseURL    string
	APIKey     string
	APISecret  string
	HTTPClient *http.Client
}

// NewAPIClient 初始化客户端
func NewAPIClient(apiKey, apiSecret string) *APIClient {
	return &APIClient{
		// BaseURL:   "https://fapi.binance.com", // U本位合约实盘地址
		APIKey:    apiKey,
		APISecret: apiSecret,
		HTTPClient: &http.Client{
			Timeout: 5 * time.Second, // 交易请求必须有超时控制
		},
	}
}

// createSignature 核心加密逻辑：HMAC-SHA256
func (c *APIClient) createSignature(queryString string) string {
	mac := hmac.New(sha256.New, []byte(c.APISecret))
	mac.Write([]byte(queryString))
	return hex.EncodeToString(mac.Sum(nil))
}

// GetUSDTBalance 主动调用 REST API 查询合约账户的 USDT 余额
func (c *APIClient) GetUSDTBalance() (string, error) {
	// 1. 构造请求参数
	params := url.Values{}
	params.Add("timestamp", fmt.Sprintf("%d", time.Now().UnixMilli()))
	params.Add("recvWindow", "5000") // 允许 5 秒的网络延迟窗口

	queryString := params.Encode()

	// 2. HMAC-SHA256 签名
	mac := hmac.New(sha256.New, []byte(c.APISecret))
	mac.Write([]byte(queryString))
	signature := hex.EncodeToString(mac.Sum(nil))

	// 3. 拼接最终请求 URL (币安 U本位合约查询余额接口为 /fapi/v2/balance)
	reqURL := fmt.Sprintf("%s/fapi/v2/balance?%s&signature=%s", c.BaseURL, queryString, signature)

	req, err := http.NewRequest(http.MethodGet, reqURL, nil)
	if err != nil {
		return "", err
	}
	// 注入 API Key
	req.Header.Set("X-MBX-APIKEY", c.APIKey)

	// 4. 发送请求并解析
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	// 如果状态码不是 200 OK，说明报错了 (比如签名错误、IP限制)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("API 报错 [%d]: %s", resp.StatusCode, string(body))
	}

	// 5. 解析 JSON 数组
	var balances []map[string]interface{}
	if err := json.Unmarshal(body, &balances); err != nil {
		return "", fmt.Errorf("JSON 解析失败: %s", string(body))
	}

	// 6. 遍历寻找 USDT 资产
	for _, b := range balances {
		if asset, ok := b["asset"].(string); ok && asset == "USDT" {
			if bal, ok := b["balance"].(string); ok {
				return bal, nil
			}
		}
	}

	return "0.00", fmt.Errorf("未找到 USDT 资产信息")
}

// GetAccountBalance 获取合约账户余额 (安全测试鉴权的最佳接口)
func (c *APIClient) GetAccountBalance() (string, error) {
	endpoint := "/fapi/v2/balance"

	// 1. 构造请求参数
	params := url.Values{}
	// 必须带上 timestamp，防重放攻击
	params.Add("timestamp", fmt.Sprintf("%d", time.Now().UnixMilli()))
	// 可选：recvWindow 防止网络延迟导致的请求过期，默认 5000ms
	params.Add("recvWindow", "5000")

	// 2. 生成签名
	queryString := params.Encode()
	signature := c.createSignature(queryString)

	// 3. 拼接最终请求 URL (包含签名)
	fullURL := fmt.Sprintf("%s%s?%s&signature=%s", c.BaseURL, endpoint, queryString, signature)

	// 4. 构造 HTTP 请求
	req, err := http.NewRequest(http.MethodGet, fullURL, nil)
	if err != nil {
		return "", err
	}

	// 5. 极其重要：在 Header 中注入 API Key
	req.Header.Add("X-MBX-APIKEY", c.APIKey)

	// 6. 发送请求
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	// 7. 读取并返回结果
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("API Error: Status %d, Response: %s", resp.StatusCode, string(bodyBytes))
	}

	// 这里为了直观，我们先直接返回一段格式化好的 JSON 字符串
	var prettyJSON interface{}
	json.Unmarshal(bodyBytes, &prettyJSON)
	output, _ := json.MarshalIndent(prettyJSON, "", "  ")

	return string(output), nil
}

// OrderRequest 包含发单所需的所有核心参数
type OrderRequest struct {
	Symbol           string  // 交易对，如 BTCUSDT
	Side             string  // 买卖方向：BUY 或 SELL
	PositionSide     string  // 持仓方向：LONG 或 SHORT (在双向持仓模式下必须填写)
	Type             string  // 订单类型：LIMIT 或 MARKET
	Quantity         float64 // 下单数量 (如 0.001 个 BTC)
	Price            float64 // 价格 (限价单必填，市价单不填)
	TimeInForce      string  // 有效方式，限价单通常填 GTC (Good Till Cancel)
	NewClientOrderID string  // [极度重要] 客户端自定义的唯一订单ID，用于防重发和回溯
}

// PlaceOrder 发送 U 本位合约订单
func (c *APIClient) PlaceOrder(req OrderRequest) (string, error) {
	endpoint := "/fapi/v1/order"

	// 1. 构造请求参数
	params := url.Values{}
	params.Add("symbol", req.Symbol)
	params.Add("side", req.Side)
	if req.PositionSide != "" {
		params.Add("positionSide", req.PositionSide)
	}
	params.Add("type", req.Type)

	// 在量化中，数量和价格必须严格按照交易所规定的精度进行格式化 (例如 BTC 通常是 3 位数小数)
	// 这里为了简化，我们先用 %f 输出，并在实盘中建议手写精度截断器
	params.Add("quantity", fmt.Sprintf("%f", req.Quantity))

	if req.Type == "LIMIT" {
		params.Add("price", fmt.Sprintf("%f", req.Price))
		params.Add("timeInForce", req.TimeInForce) // 限价单必填，如 "GTC"
	}

	// 注入客户端自定义 ID，这是量化系统的基石，防止因为网络超时导致的重复下单
	if req.NewClientOrderID != "" {
		params.Add("newClientOrderId", req.NewClientOrderID)
	}

	// 核心安全参数
	params.Add("timestamp", fmt.Sprintf("%d", time.Now().UnixMilli()))
	params.Add("recvWindow", "5000")

	// 2. 签名与拼接
	queryString := params.Encode()
	signature := c.createSignature(queryString)
	fullURL := fmt.Sprintf("%s%s?%s&signature=%s", c.BaseURL, endpoint, queryString, signature)

	// 3. 极其重要：发单必须是 HTTP POST 请求！
	httpReq, err := http.NewRequest(http.MethodPost, fullURL, nil)
	if err != nil {
		return "", err
	}

	httpReq.Header.Add("X-MBX-APIKEY", c.APIKey)

	// 4. 执行请求
	resp, err := c.HTTPClient.Do(httpReq)
	if err != nil {
		return "", err // 这里返回的通常是网络层错误，如连接超时
	}
	defer resp.Body.Close()

	// 5. 解析结果
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	if resp.StatusCode != http.StatusOK {
		// 这里返回的通常是业务层错误，比如余额不足、精度不对等
		return "", fmt.Errorf("API Error: Status %d, Response: %s", resp.StatusCode, string(bodyBytes))
	}

	var prettyJSON interface{}
	json.Unmarshal(bodyBytes, &prettyJSON)
	output, _ := json.MarshalIndent(prettyJSON, "", "  ")

	return string(output), nil
}

// CancelOrderRequest 撤单请求参数
type CancelOrderRequest struct {
	Symbol            string
	OrigClientOrderID string // [极其核心] 你发单时自定义的那个 ID
}

// CancelOrder 撤销指定的 U 本位合约订单
func (c *APIClient) CancelOrder(req CancelOrderRequest) (string, error) {
	endpoint := "/fapi/v1/order"

	// 1. 构造请求参数
	params := url.Values{}
	params.Add("symbol", req.Symbol)
	// 使用 origClientOrderId 来指定要撤销的订单
	params.Add("origClientOrderId", req.OrigClientOrderID)

	// 核心安全参数
	params.Add("timestamp", fmt.Sprintf("%d", time.Now().UnixMilli()))
	params.Add("recvWindow", "5000")

	// 2. 签名与拼接
	queryString := params.Encode()
	signature := c.createSignature(queryString)
	fullURL := fmt.Sprintf("%s%s?%s&signature=%s", c.BaseURL, endpoint, queryString, signature)

	// 3. 极其重要：撤单必须是 HTTP DELETE 请求！
	httpReq, err := http.NewRequest(http.MethodDelete, fullURL, nil)
	if err != nil {
		return "", err
	}

	httpReq.Header.Add("X-MBX-APIKEY", c.APIKey)

	// 4. 执行请求
	resp, err := c.HTTPClient.Do(httpReq)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	// 5. 解析结果
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("API Error: Status %d, Response: %s", resp.StatusCode, string(bodyBytes))
	}

	var prettyJSON interface{}
	json.Unmarshal(bodyBytes, &prettyJSON)
	output, _ := json.MarshalIndent(prettyJSON, "", "  ")

	return string(output), nil
}

// GetListenKey 向币安申请专属的私有 WebSocket 监听令牌
func (c *APIClient) GetListenKey() (string, error) {
	url := c.BaseURL + "/fapi/v1/listenKey"
	req, err := http.NewRequest(http.MethodPost, url, nil)
	if err != nil {
		return "", err
	}

	// 私有推送只需在 Header 验证 API Key
	req.Header.Set("X-MBX-APIKEY", c.APIKey)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", err
	}

	if key, ok := result["listenKey"].(string); ok {
		return key, nil
	}
	return "", fmt.Errorf("未找到 listenKey: %s", string(body))
}

// GetPosition 主动调用 REST API 查询指定交易对的当前真实持仓
func (c *APIClient) GetPosition(symbol string) (string, string, error) {
	params := url.Values{}
	params.Add("symbol", symbol)
	params.Add("timestamp", fmt.Sprintf("%d", time.Now().UnixMilli()))
	params.Add("recvWindow", "5000")

	queryString := params.Encode()
	mac := hmac.New(sha256.New, []byte(c.APISecret))
	mac.Write([]byte(queryString))
	signature := hex.EncodeToString(mac.Sum(nil))

	// 币安 U本位合约查询仓位风险接口为 /fapi/v2/positionRisk
	reqURL := fmt.Sprintf("%s/fapi/v2/positionRisk?%s&signature=%s", c.BaseURL, queryString, signature)

	req, err := http.NewRequest(http.MethodGet, reqURL, nil)
	if err != nil {
		return "0.0", "0.0", err
	}
	req.Header.Set("X-MBX-APIKEY", c.APIKey)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return "0.0", "0.0", err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "0.0", "0.0", fmt.Errorf("API 报错: %s", string(body))
	}

	var positions []map[string]interface{}
	if err := json.Unmarshal(body, &positions); err != nil {
		return "0.0", "0.0", fmt.Errorf("JSON 解析失敗")
	}

	// 🌟 同時提取 positionAmt (持倉數量) 和 entryPrice (開倉均價)
	if len(positions) > 0 {
		amt, _ := positions[0]["positionAmt"].(string)
		ep, _ := positions[0]["entryPrice"].(string)
		return amt, ep, nil
	}

	return "0.0", "0.0", nil
}
