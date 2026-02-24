# scripts/macd_strategy.py
import time
import requests
import pandas as pd

# 从基础策略模块引入基类
from strategies import BaseStrategy


class MACD5MinStrategy(BaseStrategy):
    """
    趋势跟踪：5分钟 MACD 策略 (支持完整 做多/做空/平仓)
    """

    def __init__(self, symbol, strat_config, active_env="testnet"):
        super().__init__(symbol)
        # 🌟 从配置字典中动态读取参数，如果没配则使用默认值兜底
        self.quantity = strat_config.get("quantity", 0.01)
        self.check_interval = strat_config.get("check_interval", 3.0)

        self.fast_span = strat_config.get("macd_fast", 12)
        self.slow_span = strat_config.get("macd_slow", 26)
        self.signal_span = strat_config.get("macd_signal", 9)
        self.agg = strat_config.get("aggressiveness", 5.0)  # 吃单滑点

        # 🛡️ 絕對風控參數 (預設：虧損 2% 斷臂求生，獲利 5% 落袋為安)
        self.stop_loss_pct = strat_config.get("stop_loss", 0.02)
        self.take_profit_pct = strat_config.get("take_profit", 0.05)

        self.last_check_time = 0

        if active_env == "mainnet":
            self.api_url = "https://fapi.binance.com/fapi/v1/klines"
        else:
            self.api_url = "https://testnet.binancefuture.com/fapi/v1/klines"

        self.current_trend = 0  # 记录当前趋势状态: 1 (多头/金叉), -1 (空头/死叉)

    def get_macd_trend(self):
        """极速拉取 5 分钟 K 线并计算 MACD"""
        try:
            params = {
                "symbol": self.symbol,
                "interval": "5m",
                "limit": 50  # 只需要最近 50 根来算 EMA(12, 26) 绰绰有余
            }
            resp = requests.get(self.api_url, params=params, timeout=(5.0, 10.0))

            if resp.status_code != 200:
                return 0

            data = resp.json()
            # 转换为 DataFrame 进行极速向量化运算
            df = pd.DataFrame(data, columns=['timestamp', 'open', 'high', 'low', 'close', 'volume', 'close_time', 'qav',
                                             'num_trades', 'taker_base_vol', 'taker_quote_vol', 'ignore'])
            df['close'] = df['close'].astype(float)

            # 向量化计算 MACD (Fast=12, Slow=26, Signal=9)
            exp1 = df['close'].ewm(span=self.fast_span, adjust=False).mean()
            exp2 = df['close'].ewm(span=self.slow_span, adjust=False).mean()
            macd = exp1 - exp2
            signal = macd.ewm(span=self.signal_span, adjust=False).mean()
            hist = macd - signal  # MACD 柱状图 (Histogram)

            # 取最新两根 K 线的柱状图值
            prev_hist = hist.iloc[-2]  # 前一根 (已收盘)
            curr_hist = hist.iloc[-1]  # 当前最新 (可能还在跳动)

            # 核心判断逻辑
            if prev_hist < 0 and curr_hist > 0:
                return 1  # 💥 金叉 (零轴下穿上) -> 多头信号
            elif prev_hist > 0 and curr_hist < 0:
                return -1  # 💥 死叉 (零轴上穿下) -> 空头信号
            else:
                # 没发生交叉，维持现状，看当前的柱子是红是绿
                return 1 if curr_hist > 0 else -1

        except Exception as e:
            print(f"  [⚠️ K线运算异常] {e}")
            return 0

    def on_tick(self, book, current_position=0.0, entry_price=0.0):
        """
        结合盘口与真实仓位，执行 MACD 状态机
        current_position > 0 代表持有多单
        current_position < 0 代表持有空单
        """
        current_time = time.time()

        # 1. 频率控制：每 3 秒算一次 MACD
        if current_time - self.last_check_time < self.check_interval:
            return None

        self.last_check_time = current_time

        # 2. 获取最新盘口，用于激进吃单保证成交 (Taker)
        bids = book.get("b", [])
        asks = book.get("a", [])
        if not bids or not asks:
            return None

        best_bid = float(bids[0]["p"])  # 买一价 (对手盘: 用于卖出)
        best_ask = float(asks[0]["p"])  # 卖一价 (对手盘: 用于买入)

        # ==========================================
        # 🛡️ [最高優先級] 硬核風控攔截器 (TP/SL)
        # 只要有倉位，每一幀盤口都會計算實時浮動盈虧！
        # ==========================================
        if current_position != 0 and entry_price > 0:
            # 計算實時盈虧百分比 (PnL %)
            if current_position > 0:  # 多單浮盈計算
                pnl_pct = (best_bid - entry_price) / entry_price
            else:  # 空單浮盈計算
                pnl_pct = (entry_price - best_ask) / entry_price

            # 觸發絕對止損 (Stop Loss)
            if pnl_pct <= -self.stop_loss_pct:
                print(
                    f"\n🩸 [硬核止損觸發] 當前浮虧 {pnl_pct * 100:.2f}% (大於設定的 {self.stop_loss_pct * 100}%)！無條件斷臂平倉！")
                self.last_check_time = current_time + 10.0  # 平倉後強制冷卻 10 秒
                return {
                    "symbol": self.symbol,
                    "side": "SELL" if current_position > 0 else "BUY",
                    "quantity": abs(current_position),
                    "price": best_bid - self.agg if current_position > 0 else best_ask + self.agg,
                    "reason": "Hard Stop Loss"
                }

            # 觸發絕對止盈 (Take Profit)
            elif pnl_pct >= self.take_profit_pct:
                print(f"\n💰 [硬核止盈觸發] 當前浮盈 {pnl_pct * 100:.2f}%！落袋為安！")
                self.last_check_time = current_time + 10.0
                return {
                    "symbol": self.symbol,
                    "side": "SELL" if current_position > 0 else "BUY",
                    "quantity": abs(current_position),
                    "price": best_bid - self.agg if current_position > 0 else best_ask + self.agg,
                    "reason": "Hard Take Profit"
                }

        # ==========================================
        # 🧠 如果風控沒觸發，才進入常規的 MACD 趨勢檢查
        # ==========================================
        # 3. 获取 MACD 趋势
        trend = self.get_macd_trend()

        signal_dict = None

        # ==========================================
        # 🧠 四象限策略状态机 (开多/平多/开空/平空)
        # ==========================================
        if trend == 1:
            # 📈【多头/金叉状态】
            if current_position < 0:
                print(f"\n🔄 [MACD 金叉] 趋势反转向上，立刻平掉空单！")
                signal_dict = {
                    "symbol": self.symbol,
                    "side": "BUY",  # 平空必须买入
                    "quantity": abs(current_position),
                    "price": best_ask + 5.0,  # 激进吃单
                    "reason": "MACD 金叉平空"
                }
            elif current_position == 0:
                print(f"\n🚀 [MACD 金叉] 启动多头攻势，开多！")
                signal_dict = {
                    "symbol": self.symbol,
                    "side": "BUY",
                    "quantity": self.quantity,
                    "price": best_ask + 5.0,
                    "reason": "MACD 金叉开多"
                }

        elif trend == -1:
            # 📉【空头/死叉状态】
            if current_position > 0:
                print(f"\n🔄 [MACD 死叉] 趋势反转向下，立刻平掉多单！")
                signal_dict = {
                    "symbol": self.symbol,
                    "side": "SELL",  # 平多必须卖出
                    "quantity": abs(current_position),
                    "price": best_bid - 5.0,  # 激进砸盘
                    "reason": "MACD 死叉平多"
                }
            elif current_position == 0:
                print(f"\n📉 [MACD 死叉] 启动空头打击，开空！")
                signal_dict = {
                    "symbol": self.symbol,
                    "side": "SELL",
                    "quantity": self.quantity,
                    "price": best_bid - 5.0,
                    "reason": "MACD 死叉开空"
                }

        # 如果产生交易信号，强制休眠 5 秒防止重复发单
        if signal_dict:
            self.last_check_time = current_time + 5.0

        return signal_dict