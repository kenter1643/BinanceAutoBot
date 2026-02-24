package config

import (
	"encoding/json"
	"os"
)

type Config struct {
	Binance BinanceRouter `json:"binance"`
	Redis   RedisConfig   `json:"redis"`
}

// BinanceRouter 负责路由当前激活的环境
type BinanceRouter struct {
	ActiveEnv string    `json:"active_env"`
	Symbol    string    `json:"symbol"`
	Mainnet   EnvConfig `json:"mainnet"`
	Testnet   EnvConfig `json:"testnet"`
}

// EnvConfig 具体的环境配置参数
type EnvConfig struct {
	APIKey      string `json:"api_key"`
	APISecret   string `json:"api_secret"`
	RestBaseURL string `json:"rest_base_url"`
	WSDepthURL  string `json:"ws_depth_url"`
}

type RedisConfig struct {
	Addr string `json:"addr"`
	DB   int    `json:"db"`
}

func LoadConfig(path string) (*Config, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var cfg Config
	if err := json.NewDecoder(file).Decode(&cfg); err != nil {
		return nil, err
	}

	// 环境变量优先级高于配置文件，避免明文密钥提交到代码库
	if v := os.Getenv("BINANCE_MAINNET_API_KEY"); v != "" {
		cfg.Binance.Mainnet.APIKey = v
	}
	if v := os.Getenv("BINANCE_MAINNET_API_SECRET"); v != "" {
		cfg.Binance.Mainnet.APISecret = v
	}
	if v := os.Getenv("BINANCE_TESTNET_API_KEY"); v != "" {
		cfg.Binance.Testnet.APIKey = v
	}
	if v := os.Getenv("BINANCE_TESTNET_API_SECRET"); v != "" {
		cfg.Binance.Testnet.APISecret = v
	}

	return &cfg, nil
}

// GetActiveEnv 核心的智能路由方法：根据 active_env 开关自动返回对应的配置实体
func (b *BinanceRouter) GetActiveEnv() EnvConfig {
	if b.ActiveEnv == "mainnet" {
		return b.Mainnet
	}
	// 默认退化为 testnet，这是最安全的防爆仓兜底策略
	return b.Testnet
}
