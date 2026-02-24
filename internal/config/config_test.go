package config

import (
	"os"
	"testing"
)

func TestGetActiveEnv_Testnet(t *testing.T) {
	b := BinanceRouter{
		ActiveEnv: "testnet",
		Mainnet:   EnvConfig{APIKey: "main_key"},
		Testnet:   EnvConfig{APIKey: "test_key"},
	}
	env := b.GetActiveEnv()
	if env.APIKey != "test_key" {
		t.Errorf("expected test_key, got %s", env.APIKey)
	}
}

func TestGetActiveEnv_Mainnet(t *testing.T) {
	b := BinanceRouter{
		ActiveEnv: "mainnet",
		Mainnet:   EnvConfig{APIKey: "main_key"},
		Testnet:   EnvConfig{APIKey: "test_key"},
	}
	env := b.GetActiveEnv()
	if env.APIKey != "main_key" {
		t.Errorf("expected main_key, got %s", env.APIKey)
	}
}

func TestGetActiveEnv_DefaultsToTestnet(t *testing.T) {
	b := BinanceRouter{
		ActiveEnv: "unknown",
		Mainnet:   EnvConfig{APIKey: "main_key"},
		Testnet:   EnvConfig{APIKey: "test_key"},
	}
	env := b.GetActiveEnv()
	if env.APIKey != "test_key" {
		t.Errorf("unknown env should fall back to testnet, got %s", env.APIKey)
	}
}

func TestLoadConfig_EnvVarOverride(t *testing.T) {
	// 写一个临时 config 文件
	content := `{
		"binance": {
			"active_env": "testnet",
			"symbol": "BTCUSDT",
			"mainnet": {"api_key": "file_main_key", "api_secret": "file_main_secret", "rest_base_url": "", "ws_depth_url": ""},
			"testnet": {"api_key": "file_test_key", "api_secret": "file_test_secret", "rest_base_url": "", "ws_depth_url": ""}
		},
		"redis": {"addr": "127.0.0.1:6379", "db": 0}
	}`
	f, err := os.CreateTemp("", "config_test_*.json")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())
	f.WriteString(content)
	f.Close()

	// 设置环境变量
	os.Setenv("BINANCE_TESTNET_API_KEY", "env_test_key")
	os.Setenv("BINANCE_TESTNET_API_SECRET", "env_test_secret")
	os.Setenv("BINANCE_MAINNET_API_KEY", "env_main_key")
	defer func() {
		os.Unsetenv("BINANCE_TESTNET_API_KEY")
		os.Unsetenv("BINANCE_TESTNET_API_SECRET")
		os.Unsetenv("BINANCE_MAINNET_API_KEY")
	}()

	cfg, err := LoadConfig(f.Name())
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if cfg.Binance.Testnet.APIKey != "env_test_key" {
		t.Errorf("env var should override testnet api_key, got %s", cfg.Binance.Testnet.APIKey)
	}
	if cfg.Binance.Testnet.APISecret != "env_test_secret" {
		t.Errorf("env var should override testnet api_secret, got %s", cfg.Binance.Testnet.APISecret)
	}
	if cfg.Binance.Mainnet.APIKey != "env_main_key" {
		t.Errorf("env var should override mainnet api_key, got %s", cfg.Binance.Mainnet.APIKey)
	}
	// mainnet secret 没有设置环境变量，应保留文件值
	if cfg.Binance.Mainnet.APISecret != "file_main_secret" {
		t.Errorf("file value should remain when no env var set, got %s", cfg.Binance.Mainnet.APISecret)
	}
}

func TestLoadConfig_InvalidPath(t *testing.T) {
	_, err := LoadConfig("/nonexistent/path/config.json")
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestLoadConfig_InvalidJSON(t *testing.T) {
	f, _ := os.CreateTemp("", "bad_config_*.json")
	defer os.Remove(f.Name())
	f.WriteString("{invalid json}")
	f.Close()

	_, err := LoadConfig(f.Name())
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}
