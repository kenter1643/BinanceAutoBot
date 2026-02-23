import time


class BaseStrategy:
    def __init__(self, symbol):
        self.symbol = symbol

    def on_tick(self, book, current_position):
        raise NotImplementedError("策略必须实现 on_tick 方法")


class SpreadBreakoutStrategy(BaseStrategy):
    """微观结构：价差突破策略 (带风控版)"""

    def __init__(self, symbol, threshold=0.1, quantity=0.01, cooldown=10.0, max_position=0.03):
        super().__init__(symbol)
        self.threshold = threshold
        self.quantity = quantity
        self.cooldown = cooldown
        self.max_position = max_position  # 🛡️ [新增] 最大多头持仓上限 (例如 0.03 个 BTC)
        self.last_fire_time = 0.0
        self.tick_count = 0

    def on_tick(self, book, current_position=0.0):
        self.tick_count += 1

        bids = book.get("b", [])
        asks = book.get("a", [])
        if not bids or not asks: return None

        best_bid = float(bids[0]["p"])
        best_ask = float(asks[0]["p"])
        spread = best_ask - best_bid

        # ==========================================
        # 🛡️ 绝对风控第一关：仓位超限，立刻锁死！
        # ==========================================
        if current_position >= self.max_position:
            if self.tick_count % 20 == 0:
                print(f"\n🛑 [风控拦截] 当前多头仓位 ({current_position}) 已达上限 ({self.max_position})，停止买入！")
            return None  # 直接返回 None，掐断发单信号！

        # 2. 冷却期检查
        current_time = time.time()
        is_cooling_down = (current_time - self.last_fire_time) < self.cooldown

        if is_cooling_down:
            return None

        # 3. 核心触发逻辑
        if spread >= self.threshold:
            print(
                f"\n💥 [条件成立] 价差 {spread:.2f} 满足要求，且仓位安全 ({current_position} < {self.max_position})，拔枪！")
            self.last_fire_time = current_time
            target_price = best_ask + 5.0  # 保持激进吃单

            return {
                "symbol": self.symbol,
                "side": "BUY",
                "quantity": self.quantity,
                "price": target_price,
                "reason": f"Spread={spread:.2f} | Pos={current_position}"
            }

        return None