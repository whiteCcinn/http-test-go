package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"math/rand"
	"net/http"
	"net/http/httptrace"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/guptarohit/asciigraph"
	"github.com/olekukonko/tablewriter"
	"github.com/schollz/progressbar/v3"
)

// WorkerStats 保存每个 worker 的局部统计数据
type WorkerStats struct {
	TotalRequests   int64
	SuccessRequests int64
	FailedRequests  int64
	TotalTime       time.Duration
	ResponseTimes   []time.Duration
	StatusCodes     map[int]int
}

// Stats 用于聚合统计数据
type Stats struct {
	TotalRequests   int64           // 总请求数
	SuccessRequests int64           // 成功请求数
	FailedRequests  int64           // 失败请求数
	TotalTime       time.Duration   // 总耗时
	ResponseTimes   []time.Duration // 所有请求的响应时延
	StatusCodes     map[int]int     // HTTP 状态码统计，键为状态码，值为出现次数
}

// 全局趋势数组（TPS、QPS 为数值，响应时延单位为 ms）
var (
	tpsHistory []float64
	qpsHistory []float64
	p50History []float64
	p95History []float64
	p99History []float64
)

// requestBodies 支持两种格式：
// - 只有 body，则形式为 [["body"]]
// - 有 URL 和 body，则形式为 [["url", "body"], ...]
var requestBodies [][]string

// 全局 HTTP 客户端复用
var clientKeepAlive *http.Client
var clientNoKeepAlive *http.Client

// 全局原子计数器（用于快速汇总）
var globalTotalRequests int64
var globalSuccessRequests int64
var globalFailedRequests int64

func init() {
	// 启用 Keep-Alive 的客户端
	clientKeepAlive = &http.Client{
		Transport: &http.Transport{
			DisableKeepAlives:   false,
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 100,
			IdleConnTimeout:     30 * time.Second,
		},
		Timeout: 10 * time.Second,
	}
	// 不启用 Keep-Alive 的客户端
	clientNoKeepAlive = &http.Client{
		Transport: &http.Transport{
			DisableKeepAlives: true,
		},
		Timeout: 10 * time.Second,
	}
}

func main() {
	var url string
	var concurrency int
	var totalRequests int
	var keepAliveRatio float64
	var method string
	var bodyFile string
	var reportInterval int

	flag.StringVar(&url, "url", "http://localhost:8080", "Target URL")
	flag.IntVar(&concurrency, "c", 10, "Number of concurrent workers")
	flag.IntVar(&totalRequests, "n", 100, "Total number of requests")
	flag.Float64Var(&keepAliveRatio, "keepalive_ratio", 0.7, "Ratio of requests using keep-alive (0.0 - 1.0)")
	flag.StringVar(&method, "X", "POST", "HTTP method (GET, POST, etc.)")
	flag.StringVar(&bodyFile, "bodyfile", "", "JSON file containing request bodies")
	flag.IntVar(&reportInterval, "interval", 20, "Report stats every N requests")
	flag.Parse()

	fmt.Printf("\n🌍  Target URL: %s\n", url)
	fmt.Printf("🔄  Concurrency: %d, Total Requests: %d\n", concurrency, totalRequests)
	fmt.Printf("⚡  Keep-Alive Ratio: %.2f\n", keepAliveRatio)
	fmt.Printf("📡  HTTP Method: %s\n", method)

	if bodyFile != "" {
		loadBodiesFromFile(bodyFile)
		fmt.Printf("📂  Loaded %d request bodies\n", len(requestBodies))
	}
	fmt.Println("======================================")

	bar := progressbar.Default(int64(totalRequests))

	// 创建 per-worker 统计数据，每个 worker 独占一个 WorkerStats 实例（预分配容量）
	workerStats := make([]*WorkerStats, concurrency)
	requestsPerWorker := totalRequests / concurrency
	for i := 0; i < concurrency; i++ {
		workerStats[i] = &WorkerStats{
			ResponseTimes: make([]time.Duration, 0, requestsPerWorker),
			StatusCodes:   make(map[int]int),
		}
	}

	// 启动一个 ticker，每秒聚合所有 worker 数据进行阶段性报告
	doneChan := make(chan struct{})
	var tickerWg sync.WaitGroup
	tickerWg.Add(1)
	go func() {
		defer tickerWg.Done()
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				aggStats := aggregateWorkerStats(workerStats)
				// 以当前时间作为参考，仅用于阶段性展示
				reportStats(&aggStats, time.Now())
			case <-doneChan:
				return
			}
		}
	}()

	var wg sync.WaitGroup
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			ws := workerStats[idx]
			for j := 0; j < requestsPerWorker; j++ {
				startReq := time.Now()
				reqURL, body := getRandomRequest(url)
				var client *http.Client
				if rand.Float64() < keepAliveRatio {
					client = clientKeepAlive
				} else {
					client = clientNoKeepAlive
				}
				// 使用 HTTPTrace 捕获响应首字节时间
				var startTrace time.Time
				trace := &httptrace.ClientTrace{
					GotFirstResponseByte: func() {
						startTrace = time.Now()
					},
				}
				req, err := http.NewRequest(method, reqURL, strings.NewReader(body))
				if err != nil {
					ws.FailedRequests++
					atomic.AddInt64(&globalFailedRequests, 1)
					continue
				}
				req = req.WithContext(httptrace.WithClientTrace(req.Context(), trace))
				req.Header.Set("User-Agent", "Go-HTTP-LoadTester")
				req.Header.Set("Content-Type", "application/json")
				resp, err := client.Do(req)
				var duration time.Duration
				if err != nil {
					ws.FailedRequests++
					atomic.AddInt64(&globalFailedRequests, 1)
				} else {
					_, _ = io.Copy(io.Discard, resp.Body)
					resp.Body.Close()
					if !startTrace.IsZero() {
						duration = time.Since(startTrace)
					} else {
						duration = time.Since(startReq)
					}
					if resp.StatusCode >= 200 && resp.StatusCode < 300 {
						ws.SuccessRequests++
						atomic.AddInt64(&globalSuccessRequests, 1)
					} else {
						ws.FailedRequests++
						atomic.AddInt64(&globalFailedRequests, 1)
					}
					ws.StatusCodes[resp.StatusCode]++
					ws.ResponseTimes = append(ws.ResponseTimes, duration)
				}
				ws.TotalRequests++
				atomic.AddInt64(&globalTotalRequests, 1)
				ws.TotalTime += time.Since(startReq)
				bar.Add(1)
			}
		}(i)
	}

	wg.Wait()
	close(doneChan)
	tickerWg.Wait()

	// 最终汇总所有 worker 的统计数据
	finalStats := aggregateWorkerStats(workerStats)
	endTime := time.Now()
	fmt.Println("\n======================================")
	fmt.Println("✅  Test completed! Final statistics:")
	reportStats(&finalStats, endTime)

	ensureNonEmptyHistory()

	fmt.Println("\n📈  TPS Trend:")
	fmt.Println(asciigraph.Plot(tpsHistory, asciigraph.Height(10)))

	fmt.Println("\n📊  QPS Trend:")
	fmt.Println(asciigraph.Plot(qpsHistory, asciigraph.Height(10)))

	fmt.Println("\n📉  Response Time Trend (ms):")
	fmt.Println("P50:")
	fmt.Println(asciigraph.Plot(p50History, asciigraph.Height(5)))
	fmt.Println("P95:")
	fmt.Println(asciigraph.Plot(p95History, asciigraph.Height(5)))
	fmt.Println("P99:")
	fmt.Println(asciigraph.Plot(p99History, asciigraph.Height(5)))
}

// aggregateWorkerStats 将所有 worker 的统计数据合并为全局统计数据
func aggregateWorkerStats(workers []*WorkerStats) Stats {
	global := Stats{
		StatusCodes:   make(map[int]int),
		ResponseTimes: make([]time.Duration, 0),
	}
	for _, ws := range workers {
		global.TotalRequests += ws.TotalRequests
		global.SuccessRequests += ws.SuccessRequests
		global.FailedRequests += ws.FailedRequests
		global.TotalTime += ws.TotalTime
		for code, count := range ws.StatusCodes {
			global.StatusCodes[code] += count
		}
		global.ResponseTimes = append(global.ResponseTimes, ws.ResponseTimes...)
	}
	return global
}

// loadBodiesFromFile 读取 JSON 文件，支持两种格式：
// 1. ["body1", "body2", ...]
// 2. [["url1", "body1"], ["url2", "body2"], ...]
func loadBodiesFromFile(filename string) {
	data, err := ioutil.ReadFile(filename)
	if err != nil {
		fmt.Printf("❌ Unable to read JSON file: %v\n", err)
		return
	}
	var parsed [][]string
	if err := json.Unmarshal(data, &parsed); err == nil {
		requestBodies = parsed
		return
	}
	var singleParsed []string
	if err := json.Unmarshal(data, &singleParsed); err == nil {
		for _, body := range singleParsed {
			requestBodies = append(requestBodies, []string{"", body})
		}
	}
}

// getRandomRequest 随机返回一个请求的 URL 与 body
func getRandomRequest(defaultURL string) (string, string) {
	if len(requestBodies) == 0 {
		return defaultURL, ""
	}
	randomEntry := requestBodies[rand.Intn(len(requestBodies))]
	if len(randomEntry) == 1 {
		return defaultURL, randomEntry[0]
	}
	if randomEntry[0] == "" {
		return defaultURL, randomEntry[1]
	}
	return randomEntry[0], randomEntry[1]
}

// ensureNonEmptyHistory 保证全局趋势数组不为空，防止 asciigraph.Plot 因为空切片而 panic
func ensureNonEmptyHistory() {
	if len(tpsHistory) == 0 {
		tpsHistory = append(tpsHistory, 0)
	}
	if len(qpsHistory) == 0 {
		qpsHistory = append(qpsHistory, 0)
	}
	if len(p50History) == 0 {
		p50History = append(p50History, 0)
	}
	if len(p95History) == 0 {
		p95History = append(p95History, 0)
	}
	if len(p99History) == 0 {
		p99History = append(p99History, 0)
	}
}

// percentile 计算 durations 切片中指定百分比的响应时延
func percentile(durations []time.Duration, percent float64) time.Duration {
	if len(durations) == 0 {
		return 0
	}
	index := int(float64(len(durations)) * percent / 100)
	if index >= len(durations) {
		index = len(durations) - 1
	}
	return durations[index]
}

// reportStats 输出当前统计，并更新全局趋势数组；refTime 用于计算当前时间差
func reportStats(stats *Stats, refTime time.Time) {
	totalDuration := time.Since(refTime)
	if totalDuration.Seconds() == 0 {
		return
	}
	tps := float64(stats.SuccessRequests) / totalDuration.Seconds()
	qps := float64(stats.TotalRequests) / totalDuration.Seconds()

	if len(stats.ResponseTimes) == 0 {
		fmt.Println("\n⚠️  Not enough data for statistics")
		return
	}
	sort.Slice(stats.ResponseTimes, func(i, j int) bool {
		return stats.ResponseTimes[i] < stats.ResponseTimes[j]
	})
	p50 := percentile(stats.ResponseTimes, 50)
	p95 := percentile(stats.ResponseTimes, 95)
	p99 := percentile(stats.ResponseTimes, 99)

	// 更新全局趋势数组（TPS、QPS 为数值；响应时延单位为 ms）
	tpsHistory = append(tpsHistory, tps)
	qpsHistory = append(qpsHistory, qps)
	p50History = append(p50History, float64(p50.Milliseconds()))
	p95History = append(p95History, float64(p95.Milliseconds()))
	p99History = append(p99History, float64(p99.Milliseconds()))

	table := tablewriter.NewWriter(os.Stdout)
	table.SetHeader([]string{"Metric", "Value"})
	table.Append([]string{"Total Requests", fmt.Sprintf("%d", stats.TotalRequests)})
	table.Append([]string{"Success Requests", fmt.Sprintf("%d", stats.SuccessRequests)})
	table.Append([]string{"Failed Requests", fmt.Sprintf("%d", stats.FailedRequests)})
	table.Append([]string{"TPS", fmt.Sprintf("%.2f", tps)})
	table.Append([]string{"QPS", fmt.Sprintf("%.2f", qps)})
	table.Append([]string{"P50", fmt.Sprintf("%d ms", p50.Milliseconds())})
	table.Append([]string{"P95", fmt.Sprintf("%d ms", p95.Milliseconds())})
	table.Append([]string{"P99", fmt.Sprintf("%d ms", p99.Milliseconds())})
	table.Render()

	fmt.Println("\n📡  HTTP Status Code Statistics:")
	for code, count := range stats.StatusCodes {
		fmt.Printf("  - %d: %d times\n", code, count)
	}
}
