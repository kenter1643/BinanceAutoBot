import sys
import os
import time
import unittest
from unittest.mock import patch, MagicMock

sys.path.insert(0, os.path.dirname(__file__))

from strategies import SpreadBreakoutStrategy


def make_book(bid_price, ask_price):
    return {
        "b": [{"p": str(bid_price), "q": "1.0"}],
        "a": [{"p": str(ask_price), "q": "1.0"}],
    }


class TestSpreadBreakoutStrategy(unittest.TestCase):

    def setUp(self):
        self.strat = SpreadBreakoutStrategy(
            symbol="BTCUSDT",
            threshold=10.0,
            quantity=0.01,
            cooldown=5.0,
            max_position=0.03,
        )

    def test_no_signal_when_spread_below_threshold(self):
        book = make_book(50000, 50005)  # spread=5, threshold=10
        result = self.strat.on_tick(book, current_position=0.0)
        self.assertIsNone(result)

    def test_signal_when_spread_above_threshold(self):
        book = make_book(50000, 50015)  # spread=15, threshold=10
        result = self.strat.on_tick(book, current_position=0.0)
        self.assertIsNotNone(result)
        self.assertEqual(result["side"], "BUY")
        self.assertEqual(result["symbol"], "BTCUSDT")
        self.assertEqual(result["quantity"], 0.01)
        self.assertAlmostEqual(result["price"], 50020.0)  # best_ask + 5.0

    def test_no_signal_during_cooldown(self):
        book = make_book(50000, 50015)
        self.strat.on_tick(book, current_position=0.0)  # 触发一次
        result = self.strat.on_tick(book, current_position=0.0)  # 冷却中
        self.assertIsNone(result)

    def test_signal_after_cooldown_expires(self):
        book = make_book(50000, 50015)
        self.strat.on_tick(book, current_position=0.0)
        self.strat.last_fire_time = time.time() - 10.0  # 强制过期
        result = self.strat.on_tick(book, current_position=0.0)
        self.assertIsNotNone(result)

    def test_no_signal_when_position_at_max(self):
        book = make_book(50000, 50015)
        result = self.strat.on_tick(book, current_position=0.03)  # 达到上限
        self.assertIsNone(result)

    def test_no_signal_when_position_exceeds_max(self):
        book = make_book(50000, 50015)
        result = self.strat.on_tick(book, current_position=0.05)
        self.assertIsNone(result)

    def test_no_signal_empty_book(self):
        result = self.strat.on_tick({"b": [], "a": []}, current_position=0.0)
        self.assertIsNone(result)

    def test_no_signal_missing_book_keys(self):
        result = self.strat.on_tick({}, current_position=0.0)
        self.assertIsNone(result)

    def test_tick_count_increments(self):
        book = make_book(50000, 50001)
        self.strat.on_tick(book)
        self.strat.on_tick(book)
        self.assertEqual(self.strat.tick_count, 2)


class TestMACD5MinStrategy(unittest.TestCase):

    def setUp(self):
        from macd_strategy import MACD5MinStrategy
        self.strat = MACD5MinStrategy(
            symbol="BTCUSDT",
            strat_config={
                "quantity": 0.01,
                "check_interval": 0.0,  # 不限频率，方便测试
                "macd_fast": 12,
                "macd_slow": 26,
                "macd_signal": 9,
                "aggressiveness": 5.0,
                "stop_loss": 0.02,
                "take_profit": 0.05,
            },
            active_env="testnet",
        )

    def _make_book(self, bid, ask):
        return {
            "b": [{"p": str(bid), "q": "1.0"}],
            "a": [{"p": str(ask), "q": "1.0"}],
        }

    def test_stop_loss_long(self):
        """多单亏损超过 2% 应触发止损"""
        book = self._make_book(49000, 49001)
        result = self.strat.on_tick(book, current_position=0.01, entry_price=50000.0)
        # pnl = (49000 - 50000) / 50000 = -2%，刚好触发
        self.assertIsNotNone(result)
        self.assertEqual(result["reason"], "Hard Stop Loss")
        self.assertEqual(result["side"], "SELL")

    def test_take_profit_long(self):
        """多单盈利超过 5% 应触发止盈"""
        book = self._make_book(52600, 52601)
        result = self.strat.on_tick(book, current_position=0.01, entry_price=50000.0)
        # pnl = (52600 - 50000) / 50000 = 5.2%
        self.assertIsNotNone(result)
        self.assertEqual(result["reason"], "Hard Take Profit")
        self.assertEqual(result["side"], "SELL")

    def test_stop_loss_short(self):
        """空单亏损超过 2% 应触发止损"""
        book = self._make_book(50999, 51001)
        result = self.strat.on_tick(book, current_position=-0.01, entry_price=50000.0)
        # pnl = (50000 - 51001) / 50000 = -2.002%
        self.assertIsNotNone(result)
        self.assertEqual(result["reason"], "Hard Stop Loss")
        self.assertEqual(result["side"], "BUY")

    def test_take_profit_short(self):
        """空单盈利超过 5% 应触发止盈"""
        book = self._make_book(47400, 47401)
        result = self.strat.on_tick(book, current_position=-0.01, entry_price=50000.0)
        # pnl = (50000 - 47401) / 50000 = 5.198%
        self.assertIsNotNone(result)
        self.assertEqual(result["reason"], "Hard Take Profit")
        self.assertEqual(result["side"], "BUY")

    def test_no_risk_trigger_within_range(self):
        """盈亏在范围内，不触发风控，进入 MACD 逻辑"""
        book = self._make_book(50100, 50101)
        with patch.object(self.strat, "get_macd_trend", return_value=0):
            result = self.strat.on_tick(book, current_position=0.01, entry_price=50000.0)
        self.assertIsNone(result)

    def test_macd_golden_cross_open_long(self):
        """无仓位时金叉应开多"""
        book = self._make_book(50000, 50001)
        with patch.object(self.strat, "get_macd_trend", return_value=1):
            result = self.strat.on_tick(book, current_position=0.0, entry_price=0.0)
        self.assertIsNotNone(result)
        self.assertEqual(result["side"], "BUY")
        self.assertEqual(result["reason"], "MACD 金叉开多")

    def test_macd_death_cross_open_short(self):
        """无仓位时死叉应开空"""
        book = self._make_book(50000, 50001)
        with patch.object(self.strat, "get_macd_trend", return_value=-1):
            result = self.strat.on_tick(book, current_position=0.0, entry_price=0.0)
        self.assertIsNotNone(result)
        self.assertEqual(result["side"], "SELL")
        self.assertEqual(result["reason"], "MACD 死叉开空")

    def test_macd_golden_cross_close_short(self):
        """持空单时金叉应平空"""
        book = self._make_book(50000, 50001)
        with patch.object(self.strat, "get_macd_trend", return_value=1):
            result = self.strat.on_tick(book, current_position=-0.01, entry_price=50000.0)
        self.assertIsNotNone(result)
        self.assertEqual(result["side"], "BUY")
        self.assertEqual(result["reason"], "MACD 金叉平空")

    def test_macd_death_cross_close_long(self):
        """持多单时死叉应平多"""
        book = self._make_book(50000, 50001)
        with patch.object(self.strat, "get_macd_trend", return_value=-1):
            result = self.strat.on_tick(book, current_position=0.01, entry_price=50000.0)
        self.assertIsNotNone(result)
        self.assertEqual(result["side"], "SELL")
        self.assertEqual(result["reason"], "MACD 死叉平多")

    def test_check_interval_throttle(self):
        """频率控制：check_interval 内不重复计算"""
        self.strat.check_interval = 60.0
        self.strat.last_check_time = time.time()
        book = self._make_book(50000, 50001)
        result = self.strat.on_tick(book, current_position=0.0, entry_price=0.0)
        self.assertIsNone(result)

    def test_get_macd_trend_uses_config_spans(self):
        """MACD 计算应使用配置的 span 参数而非硬编码"""
        self.assertEqual(self.strat.fast_span, 12)
        self.assertEqual(self.strat.slow_span, 26)
        self.assertEqual(self.strat.signal_span, 9)

    def test_api_url_mainnet(self):
        from macd_strategy import MACD5MinStrategy
        s = MACD5MinStrategy("BTCUSDT", {}, active_env="mainnet")
        self.assertIn("fapi.binance.com", s.api_url)

    def test_api_url_testnet(self):
        from macd_strategy import MACD5MinStrategy
        s = MACD5MinStrategy("BTCUSDT", {}, active_env="testnet")
        self.assertIn("testnet", s.api_url)


if __name__ == "__main__":
    unittest.main()
