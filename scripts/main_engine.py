import redis
import json
import time
import os
import sys
import requests_unixsocket

from macd_strategy import MACD5MinStrategy
# 导入策略模块
from strategies import SpreadBreakoutStrategy


class QuantEngine:
    def __init__(self):
        self.config = self._load_config()
        self.symbol = self.config['binance']['symbol']
        self.redis_client = self._init_redis()
        self.session = requests_unixsocket.Session()
        self.uds_url = 'http+unix://%2Ftmp%2Fquant_engine.sock/api/order'

        # ==========================================
        # 🚨 就是这里！必须先定义 strat_config 变量
        # 试图从 config.json 获取 "strategy" 节点，如果获取不到就给个空字典 {}兜底
        # ==========================================
        strat_config = self.config.get('strategy', {})
        active_env = self.config['binance']['active_env']

        # 这样下面在传参的时候，IDE 就认得 strat_config 了！
        self.strategy = MACD5MinStrategy(
            symbol=self.symbol,
            strat_config=strat_config,  # <--- 这里就不会再报红线了
            active_env=active_env
        )

        # 🌟 【关键修改】将阈值降到 0.1，保证只要有盘口就百分百触发！
        """
        self.strategy = SpreadBreakoutStrategy(
            symbol=self.symbol,
            threshold=0.1,  # <--- 极低阈值测试
            quantity=0.01,
            cooldown=10.0
        )
        """


    def _load_config(self):
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
        print(f"🎯 [主引擎] 正在下达指令: 以 {signal['price']:.2f} 做多 {signal['quantity']} {signal['symbol']}")

        payload = {
            "side": signal['side'],
            "quantity": signal['quantity'],
            "price": signal['price']
        }

        try:
            start_t = time.perf_counter()
            resp = self.session.post(self.uds_url, json=payload, timeout=2.0)
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

        try:
            while True:
                raw_data = self.redis_client.get(redis_key)
                if not raw_data:
                    time.sleep(0.05)
                    continue

                book = json.loads(raw_data)
                current_id = book.get("u")

                if current_id == last_update_id:
                    time.sleep(0.005)
                    continue
                last_update_id = current_id

                # 🌟 [新增] 从 Redis 极速读取 Go 网关同步过来的真实仓位
                pos_key = f"Position:{self.symbol}"
                pos_str = self.redis_client.get(pos_key)

                # 币安推送的仓位是字符串形式的数字，如果是空说明还没建仓
                current_position = float(pos_str) if pos_str else 0.0

                # 终端心跳展示：把仓位也打印出来
                if current_id % 10 == 0:
                    bids = book.get("b", [])
                    asks = book.get("a", [])
                    if bids and asks:
                        sys.stdout.write(
                            f"\r[{current_id}] 盘口心跳... 买一:{bids[0]['p']} | 卖一:{asks[0]['p']} | 📦 当前仓位: {current_position}   ")
                        sys.stdout.flush()

                # 💡 核心修改：把当前的真实仓位也传给策略大脑！
                signal = self.strategy.on_tick(book, current_position)

                # 把盘口数据喂给策略大脑，获取信号
                # signal = self.strategy.on_tick(book)

                # 如果策略决定开火，交由执行路由处理
                if signal:
                    self.execute_signal(signal)

        except KeyboardInterrupt:
            print("\n🛑 接收到退出信号，主引擎安全停机。")


if __name__ == "__main__":
    engine = QuantEngine()
    engine.run()