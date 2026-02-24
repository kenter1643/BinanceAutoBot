import redis
import json
import time
import os
import sys
import requests_unixsocket

from macd_strategy import MACD5MinStrategy
from strategies import SpreadBreakoutStrategy  # noqa: F401 - available for strategy switching
from obi_strategy import OBIMomentumStrategy


class QuantEngine:
    def __init__(self):
        self.config = self._load_config()
        self.symbol = self.config['binance']['symbol']
        self.redis_client = self._init_redis()
        self.session = requests_unixsocket.Session()
        self.uds_url = 'http+unix://%2Ftmp%2Fquant_engine.sock/api/order'

        strat_config = self.config.get('strategy', {})
        active_env = self.config['binance']['active_env']
        self.last_print_time = 0.0

        self.strategy = OBIMomentumStrategy(
            symbol=self.symbol,
            strat_config=strat_config,
            active_env=active_env
        )

    @staticmethod
    def _load_config():
        config_path = os.path.join(os.path.dirname(__file__), '..', 'config.json')
        try:
            with open(config_path, 'r', encoding='utf-8') as f:
                return json.load(f)
        except Exception as e:
            print(f"❌ 配置加载失败: {e}")
            sys.exit(1)

    def _init_redis(self):
        host, port = self.config['redis']['addr'].split(':')
        db = self.config['redis']['db']
        try:
            r = redis.Redis(host=host, port=int(port), db=db, decode_responses=True)
            r.ping()
            return r
        except Exception as e:
            print(f"❌ Redis 连接失败: {e}")
            sys.exit(1)

    def execute_signal(self, signal):
        """执行策略模块产生的标准交易信号"""
        print(f"\n🚨 [主引擎] 接收到开火信号: {signal['reason']}")
        print(f"🎯 [主引擎] 正在下达指令: {signal['side']} {signal['quantity']} {self.symbol} @ {signal['price']:.2f}")

        payload = {
            # 🌟 修复 1：干掉硬编码的 "BTCUSDT"，直接使用初始化时读取的 self.symbol
            "symbol": self.symbol,
            "side": signal['side'],
            "quantity": signal['quantity'],
            "price": signal['price']
        }

        try:
            start_t = time.perf_counter()
            # 🌟 修复 2：把底层 UDS 通信的超时时间从 2.0 延长到 10.0，防止 Testnet 偶尔卡顿导致误判
            resp = self.session.post(self.uds_url, json=payload, timeout=10.0)
            latency = (time.perf_counter() - start_t) * 1000

            if resp.status_code == 200:
                order_id = resp.json().get('clientOrderId', '未知')
                print(f"✅ [执行成功] IPC+网络耗时: {latency:.2f}ms | 订单号: {order_id}\n")
            else:
                print(f"❌ [执行失败] HTTP {resp.status_code} - {resp.text}\n")
        except Exception as e:
            print(f"🚨 [UDS 通信异常] {e}\n")

    def run(self):
        print(f"🚀 量化主引擎启动 | 当前环境: {self.config['binance']['active_env'].upper()}")
        # 🌟 动态打印当前策略的所有配置参数，不再写死具体的属性名
        strat_params = self.config.get('strategy', {})
        print(f"🧠 挂载策略: {self.strategy.__class__.__name__} | 动态参数: {strat_params}")
        print("📡 数据管道畅通，引擎开始运转...\n")

        redis_key = f"OrderBook:{self.symbol}"
        last_update_id = 0

        # ==========================================
        # ⏱️ 新增：性能与延迟监控探针
        # ==========================================
        tick_count = 0
        monitor_start_time = time.time()
        last_tick_time = time.time()

        try:
            while True:
                try:
                    raw_data = self.redis_client.get(redis_key)
                    if not raw_data:
                        time.sleep(0.01)  # 稍微降低睡眠时间，提高轮询精度
                        continue

                    book = json.loads(raw_data)
                    current_id = book.get("u")

                    if current_id == last_update_id:
                        time.sleep(0.005)
                        continue

                    # 🚀 计算单次 Tick 间隔
                    now = time.time()
                    tick_interval_ms = (now - last_tick_time) * 1000
                    last_tick_time = now
                    tick_count += 1

                    last_update_id = current_id

                    # ⏱️ 每隔 60 秒，打印一次系统的真实吞吐量！
                    if now - monitor_start_time >= 60.0:
                        sys.stdout.write(
                            f"\r⚡ [性能监控] 过去60秒处理 {tick_count} 帧 | 平均延迟: {60000 / tick_count if tick_count else 0:.1f}ms/帧    \n")
                        sys.stdout.flush()
                        tick_count = 0
                        monitor_start_time = now

                    # 从 Redis 读取真实仓位和开仓均价
                    pos_key = f"Position:{self.symbol}"
                    pos_str = self.redis_client.get(pos_key)

                    ep_key = f"EntryPrice:{self.symbol}"
                    ep_str = self.redis_client.get(ep_key)

                    current_position = float(pos_str) if pos_str else 0.0
                    entry_price = float(ep_str) if ep_str else 0.0

                    # 使用时间控制心跳打印
                    now = time.time()
                    if now - getattr(self, 'last_print_time', 0) > 1.0:
                        bids = book.get("b", [])
                        asks = book.get("a", [])
                        if bids and asks:
                            best_bid = float(bids[0]['p'])
                            best_ask = float(asks[0]['p'])

                            pnl_display = ""
                            if current_position != 0 and entry_price > 0:
                                if current_position > 0:
                                    pnl_usdt = (best_bid - entry_price) * current_position
                                    pnl_pct = (best_bid - entry_price) / entry_price * 100
                                else:
                                    pnl_usdt = (entry_price - best_ask) * abs(current_position)
                                    pnl_pct = (entry_price - best_ask) / entry_price * 100

                                if pnl_usdt >= 0:
                                    pnl_display = f" | 🟢 浮盈: +{pnl_usdt:.2f} USDT (+{pnl_pct:.2f}%)"
                                else:
                                    pnl_display = f" | 🔴 浮亏: {pnl_usdt:.2f} USDT ({pnl_pct:.2f}%)"

                            sys.stdout.write(
                                f"\rtime:{now}[{current_id}] 买一:{best_bid} | 卖一:{best_ask} | 📦 仓位: {current_position} (均价:{entry_price:.2f}){pnl_display}    ")
                            sys.stdout.flush()
                            self.last_print_time = now

                    signal = self.strategy.on_tick(book, current_position, entry_price)

                    if signal:
                        self.execute_signal(signal)

                except KeyboardInterrupt:
                    raise
                except Exception as e:
                    print(f"\n⚠️ [主循环异常] {e}，继续运行...")
                    time.sleep(0.1)

        except KeyboardInterrupt:
            print("\n🛑 接收到退出信号，主引擎安全停机。")


if __name__ == "__main__":
    engine = QuantEngine()
    engine.run()