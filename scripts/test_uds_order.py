# scripts/test_uds_order.py
import os
import json
import redis
import requests_unixsocket


def load_config():
    # 🌟 动态计算绝对路径：无论在哪运行，都能精准定位到项目根目录的 config.json
    current_dir = os.path.dirname(os.path.abspath(__file__))
    project_root = os.path.dirname(current_dir)
    config_path = os.path.join(project_root, 'config.json')

    with open(config_path, 'r') as f:
        return json.load(f)


def main():
    # 1. 加载全局配置
    try:
        config = load_config()
        symbol = config['binance']['symbol']
        redis_addr = config['redis']['addr']
        redis_host, redis_port = redis_addr.split(':')
        redis_db = config['redis']['db']
    except Exception as e:
        print(f"❌ 读取 config.json 失败: {e}")
        return

    # 2. 连接 Redis
    try:
        # decode_responses=True 会自动将 Redis 的 bytes 解码为 string
        rdb = redis.Redis(host=redis_host, port=int(redis_port), db=redis_db, decode_responses=True)
        rdb.ping()
        print(f"✅ Redis 连接成功，准备拉取 [{symbol}] 最新盘口...")
    except Exception as e:
        print(f"❌ Redis 连接失败: {e}")
        return

    # 3. 极速读取 Redis 中的最新盘口切片
    redis_key = f"OrderBook:{symbol}"
    ob_json = rdb.get(redis_key)
    if not ob_json:
        print(f"❌ 无法从 Redis 获取盘口数据，请确保 Go 主网关正在运行并且已写入数据！")
        return

    # 4. 解析盘口数据并计算开火价
    ob_data = json.loads(ob_json)
    asks = ob_data.get('a', [])
    if not asks:
        print("❌ 盘口 Ask(卖盘) 数据为空！")
        return

    # 🌟 提取真实卖一价 (Go 网关已经优化为存入 float64，Python 这里直接拿来用即可)
    best_ask = float(asks[0]['p'])

    # 计算激进吃单价 (加 5.0 滑点，保证 Taker 瞬间成交)
    target_price = best_ask + 5.0

    print(f"📊 当前 [{symbol}] 真实卖一价: {best_ask:.2f}")
    print(f"🎯 决定使用激进吃单价: {target_price:.2f}")

    # 5. 组装发单指令
    payload = {
        "symbol": symbol,
        "side": "BUY",
        "quantity": 0.01,
        "price": target_price
    }

    # 6. 构建 UDS 会话并发送极速请求
    session = requests_unixsocket.Session()
    uds_url = 'http+unix://%2Ftmp%2Fquant_engine.sock/api/order'

    print("🚀 正在通过底层 UDS 管道发送下单指令...")

    try:
        # 🌟 修复 Testnet 延迟暗坑：将 timeout 设定为 10 秒
        response = session.post(uds_url, json=payload, timeout=10.0)

        if response.status_code == 200:
            print(f"✅ UDS 极速下单测试成功! 网关返回:\n{json.dumps(response.json(), indent=2)}")
        else:
            print(f"⚠️ 下单被拒! 状态码: {response.status_code}\n返回: {response.text}")

    except Exception as e:
        print(f"❌ UDS 发单请求失败 (请确认网关 /tmp/quant_engine.sock 正常监听): {e}")


if __name__ == "__main__":
    main()