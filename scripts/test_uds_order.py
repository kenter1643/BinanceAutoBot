import requests_unixsocket
import json
import time


def fire_order():
    print("🚀 准备通过 UDS 通道发射订单...")

    # 初始化 UDS 会话
    session = requests_unixsocket.Session()

    # URL 格式很特殊：把 /tmp/quant_engine.sock 里的斜杠 / 替换成 %2F
    url = 'http+unix://%2Ftmp%2Fquant_engine.sock/api/order'

    # 构造发单指令 (测试网挂个不会成交的低价多单)
    payload = {
        "side": "BUY",
        "quantity": 0.01,
        "price": 28000.0
    }

    print(f"🔫 正在发射: {payload}")

    start_time = time.perf_counter()

    try:
        # 发送 POST 请求到 Go 网关
        response = session.post(url, json=payload)

        end_time = time.perf_counter()
        latency_ms = (end_time - start_time) * 1000

        print(f"⏱️ 往返总耗时 (含币安网络请求): {latency_ms:.2f} ms")

        if response.status_code == 200:
            print("✅ 收到 Go 网关回执，发单成功！")
            print(json.dumps(response.json(), indent=2))
        else:
            print(f"❌ 发单失败，状态码: {response.status_code}")
            print(response.text)

    except Exception as e:
        print(f"🚨 通信发生异常: {e}")


if __name__ == "__main__":
    fire_order()