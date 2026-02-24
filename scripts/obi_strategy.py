# scripts/obi_strategy.py
import time
from collections import deque


class OBIMomentumStrategy:
    def __init__(self, symbol, strat_config, active_env):
        self.symbol = symbol
        self.quantity = strat_config.get('quantity', 0.01)

        # 核心参数
        self.depth_levels = strat_config.get('depth_levels', 5)  # 只看前 5 档盘口 (最真实的交火区)
        self.obi_threshold = strat_config.get('obi_threshold', 0.6)  # 失衡阈值：超过 60% 的一边倒才开火

        # 硬核风控 (必须带，高频策略容错率低)
        self.stop_loss = strat_config.get('stop_loss', 0.005)  # 0.5% 极速止损
        self.take_profit = strat_config.get('take_profit', 0.01)  # 1.0% 极速止盈

        # 冷却与防抖机制
        self.last_trade_time = 0
        self.cooldown_seconds = 10  # 开火后强制冷静 10 秒，防止被插针反复清算
        self.price_history = deque(maxlen=10)  # 记录最近 10 个 tick 的价格，判断微观趋势

    def calculate_obi(self, bids, asks):
        """计算订单簿失衡度 OBI ∈ [-1, 1]"""
        # 只取前 N 档盘口
        top_bids = bids[:self.depth_levels]
        top_asks = asks[:self.depth_levels]

        # 计算买卖方在前 N 档的总挂单量 (Volume = Price * Qty)
        bid_vol = sum(float(b['p']) * float(b['q']) for b in top_bids)
        ask_vol = sum(float(a['p']) * float(a['q']) for a in top_asks)

        total_vol = bid_vol + ask_vol
        if total_vol == 0:
            return 0.0

        return (bid_vol - ask_vol) / total_vol

    def on_tick(self, book, current_position, entry_price):
        """主引擎每刷新一次盘口，就会调用此方法"""
        bids = book.get("b", [])
        asks = book.get("a", [])

        if not bids or not asks:
            return None

        best_bid = float(bids[0]['p'])
        best_ask = float(asks[0]['p'])
        mid_price = (best_bid + best_ask) / 2.0

        # 记录微观价格轨迹
        self.price_history.append(mid_price)

        # ==========================================
        # 🛡️ 第一优先级：硬核极速风控 (止盈止损)
        # ==========================================
        if current_position != 0 and entry_price > 0:
            if current_position > 0:  # 多单在手
                pnl_pct = (best_bid - entry_price) / entry_price
                if pnl_pct <= -self.stop_loss:
                    return {"reason": f"🔴 [多单止损] 浮亏达到 {pnl_pct * 100:.2f}%", "side": "SELL",
                            "quantity": abs(current_position), "price": best_bid - 5.0}  # 砸盘平仓
                elif pnl_pct >= self.take_profit:
                    return {"reason": f"🟢 [多单止盈] 浮盈达到 {pnl_pct * 100:.2f}%", "side": "SELL",
                            "quantity": abs(current_position), "price": best_bid - 5.0}

            else:  # 空单在手
                pnl_pct = (entry_price - best_ask) / entry_price
                if pnl_pct <= -self.stop_loss:
                    return {"reason": f"🔴 [空单止损] 浮亏达到 {pnl_pct * 100:.2f}%", "side": "BUY",
                            "quantity": abs(current_position), "price": best_ask + 5.0}  # 扫货平仓
                elif pnl_pct >= self.take_profit:
                    return {"reason": f"🟢 [空单止盈] 浮盈达到 {pnl_pct * 100:.2f}%", "side": "BUY",
                            "quantity": abs(current_position), "price": best_ask + 5.0}

            # 有仓位且未触发风控时，死等，不乱开新仓
            return None

        # ==========================================
        # ⚔️ 第二优先级：捕捉盘口失衡，发起狙击
        # ==========================================
        now = time.time()
        if now - self.last_trade_time < self.cooldown_seconds:
            return None  # 冷却中，不开枪

        # 计算当前 OBI
        obi = self.calculate_obi(bids, asks)

        # 判断微观动能 (当前价格是否高于几微秒前的价格)
        if len(self.price_history) == self.price_history.maxlen:
            short_term_trend = mid_price - self.price_history[0]

            # 策略条件 1：买盘碾压 (OBI > 0.6) 且 价格微观抬升 -> 抢多！
            if obi > self.obi_threshold and short_term_trend > 0:
                self.last_trade_time = now
                return {
                    "reason": f"🚀 [买盘碾压] OBI={obi:.2f} 出现巨量托单，顺势吃多",
                    "side": "BUY",
                    "quantity": self.quantity,
                    "price": best_ask + 2.0  # 加微小滑点，保证做 Taker 瞬间成交
                }

            # 策略条件 2：卖盘泰山压顶 (OBI < -0.6) 且 价格微观下挫 -> 砸空！
            elif obi < -self.obi_threshold and short_term_trend < 0:
                self.last_trade_time = now
                return {
                    "reason": f"📉 [卖盘压顶] OBI={obi:.2f} 出现巨量压单，顺势做空",
                    "side": "SELL",
                    "quantity": self.quantity,
                    "price": best_bid - 2.0
                }

        return None