# HTTP Load Testing Tool

This is a simple HTTP load testing tool written in Go. It supports features such as measuring **TPS (Transactions Per Second)**, **QPS (Queries Per Second)**, response time percentiles (**P50**, **P95**, **P99**), and provides detailed statistics and trends via ASCII graphs.

The tool can also be configured to support **Keep-Alive** for some requests, and it allows you to provide a **JSON file** containing body data for requests.

---

## Features

- **TPS (Transactions Per Second)**: Measures the successful requests handled per second.
- **QPS (Queries Per Second)**: Measures the total number of requests per second, regardless of whether they succeed or fail.
- **Response Time Percentiles**: Measures the 50th (P50), 95th (P95), and 99th (P99) percentiles of the request response time.
- **Real-Time Statistics**: Displays metrics such as TPS, QPS, P50, P95, and P99.
- **ASCII Graphs**: Real-time trends of TPS, QPS, and response times.
- **Keep-Alive Support**: Ability to specify the ratio of requests using HTTP Keep-Alive.
- **Request Body from JSON File**: Supports loading request bodies from a JSON file for multiple requests.

---

## Installation

### Prerequisites

- **Go** 1.18 or higher

### Steps to install:

1. Clone the repository:
```bash
   git clone https://github.com/your-username/http-bench.git
   cd http-bench
```
2. Build the executable:
```
go build -o http_bench
```
3. Run the tool with the desired parameters.

## Usage

### Command-Line Arguments
- -url: The target URL (default is http://localhost:8080).
- -c: Number of concurrent requests (default is 10).
- -n: Total number of requests to send (default is 100).
- -keepalive_ratio: The percentage of requests to use Keep-Alive (default is 0.7).
- -X: The HTTP method to use (default is POST).
- -bodyfile: Path to a JSON file containing request bodies. The JSON file can be in two formats:
- ["body1", "body2", ...]
- [["url1", "body1"], ["url2", "body2"], ...]
- -interval: The number of requests after which to report statistics (default is 20).

## Example 1: Run a test with a single URL and body

```shell
./http_bench -url http://example.com -c 20 -n 500 -keepalive_ratio 0.5 -interval 50 -bodyfile requests.json
```

Where requests.json could be like:

```shell
[
    ["http://example.com/api1", "{\"name\": \"Alice\"}"],
    ["http://example.com/api2", "{\"name\": \"Bob\"}"]
]
```

## Example 2: Run a test with a default URL and multiple request bodies

```shell
./http_bench -c 20 -n 500 -keepalive_ratio 0.5 -interval 50 -bodyfile bodies.json
```

Where bodies.json could be like:

```shell
[
    "{\"name\": \"Alice\"}",
    "{\"name\": \"Bob\"}"
]
```

# Output

The tool will output statistics such as:

```
🌍  目标 URL: http://example.com
🔄  并发数: 20, 总请求数: 500
⚡  Keep-Alive 比例: 0.50
📡  HTTP 方法: POST
======================================

📡  HTTP 状态码统计:
  - 200: 800 次
  90% |███████████████████████████████████████████████████████████████████████████████████████████████████████████████████████████████████████████████                | (900/1000, 60 it/s) [15s:1s]+------------------+--------+
|      METRIC      | VALUE  |
+------------------+--------+
| Total Requests   |    900 |
| Success Requests |    900 |
| Failed Requests  |      0 |
| TPS              |  57.21 |
| QPS              |  57.21 |
| P50              | 145 ms |
| P95              | 308 ms |
| P99              | 505 ms |
+------------------+--------+

📡  HTTP 状态码统计:
  - 200: 900 次
 100% |██████████████████████████████████████████████████████████████████████████████████████████████████████████████████████████████████████████████████████████████| (1000/1000, 54 it/s)         
+------------------+--------+
|      METRIC      | VALUE  |
+------------------+--------+
| Total Requests   |   1000 |
| Success Requests |   1000 |
| Failed Requests  |      0 |
| TPS              |  54.10 |
| QPS              |  54.10 |
| P50              | 145 ms |
| P95              | 302 ms |
| P99              | 505 ms |
+------------------+--------+

📡  HTTP 状态码统计:
  - 200: 1000 次

======================================
✅  测试完成！最终统计结果：
+------------------+--------+
|      METRIC      | VALUE  |
+------------------+--------+
| Total Requests   |   1000 |
| Success Requests |   1000 |
| Failed Requests  |      0 |
| TPS              |  54.10 |
| QPS              |  54.10 |
| P50              | 145 ms |
| P95              | 302 ms |
| P99              | 505 ms |
+------------------+--------+

📡  HTTP 状态码统计:
  - 200: 1000 次

📈  TPS 变化趋势:
 57.21 ┤       ╭╮
 55.61 ┤    ╭──╯│
 54.01 ┤   ╭╯   ╰─
 52.41 ┤  ╭╯
 50.81 ┤ ╭╯
 49.21 ┤ │
 47.61 ┤╭╯
 46.01 ┤│
 44.41 ┤│
 42.81 ┤│
 41.21 ┼╯

📊  QPS 变化趋势:
 57.21 ┤       ╭╮
 55.61 ┤    ╭──╯│
 54.01 ┤   ╭╯   ╰─
 52.41 ┤  ╭╯
 50.81 ┤ ╭╯
 49.21 ┤ │
 47.61 ┤╭╯
 46.01 ┤│
 44.41 ┤│
 42.81 ┤│
 41.21 ┼╯

📉  响应时间趋势 (ms):
P50:
 181 ┼╮
 174 ┤│
 167 ┤│
 159 ┤│╭╮
 152 ┤╰╯╰─╮
 145 ┤    ╰─────
P95:
 516 ┼╮
 473 ┤│
 430 ┤╰╮
 388 ┤ ╰╮
 345 ┤  ╰───╮
 302 ┤      ╰───
P99:
 781 ┼╮
 726 ┤│
 671 ┤╰╮
 615 ┤ ╰╮
 560 ┤  ╰╮
 505 ┤   ╰──────
```

Output Explanation:
- Total Requests: Total number of requests made.
- Success Requests: Number of successful requests (HTTP status 2xx).
- Failed Requests: Number of failed requests (non-2xx status codes).
- TPS: Transactions Per Second (successful requests / total duration).
- QPS: Queries Per Second (total requests / total duration).
- P50: The 50th percentile of the response time.
- P95: The 95th percentile of the response time.
- P99: The 99th percentile of the response time.
- TPS & QPS Trends: ASCII graphs showing how TPS and QPS change over time.
- Response Time Trends: ASCII graphs showing how P50, P95, and P99 change over time.
