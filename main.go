package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"math/rand"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/guptarohit/asciigraph"
	"github.com/olekukonko/tablewriter"
	"github.com/schollz/progressbar/v3"
)

// Stats 保存了压测的统计数据
type Stats struct {
	TotalRequests   int64
	SuccessRequests int64
	FailedRequests  int64
	TotalTime       time.Duration
	ResponseTimes   []time.Duration
	StatusCodes     map[int]int
}

var (
	// requestBodies 支持二维数组：如果只有 body，则形式为 [["body"]]
	// 如果有 url 和 body，则形式为 [["url", "body"], ...]
	requestBodies [][]string

	mu sync.Mutex

	bar *progressbar.ProgressBar

	// 用于记录阶段性指标，后续展示折线图
	tpsHistory []float64
	qpsHistory []float64
	p50History []float64
	p95History []float64
	p99History []float64
)

func main() {
	var url string
	var concurrency int
	var totalRequests int
	var keepAliveRatio float64
	var method string
	var bodyFile string
	var reportInterval int

	flag.StringVar(&url, "url", "https://www.baidu.com", "请求的 URL")
	flag.IntVar(&concurrency, "c", 10, "并发数")
	flag.IntVar(&totalRequests, "n", 1000, "请求总数")
	flag.Float64Var(&keepAliveRatio, "keepalive_ratio", 0.7, "使用 Keep-Alive 的比例 (0.0 - 1.0)")
	flag.StringVar(&method, "X", "POST", "HTTP 方法 (GET, POST, etc.)")
	flag.StringVar(&bodyFile, "bodyfile", "", "包含多个请求体的 JSON 文件")
	flag.IntVar(&reportInterval, "interval", 100, "每隔多少个请求输出一次统计")
	flag.Parse()

	fmt.Printf("\n🌍  目标 URL: %s\n", url)
	fmt.Printf("🔄  并发数: %d, 总请求数: %d\n", concurrency, totalRequests)
	fmt.Printf("⚡  Keep-Alive 比例: %.2f\n", keepAliveRatio)
	fmt.Printf("📡  HTTP 方法: %s\n", method)

	if bodyFile != "" {
		loadBodiesFromFile(bodyFile)
		fmt.Printf("📂  已加载 %d 个请求体\n", len(requestBodies))
	}
	fmt.Println("======================================")

	bar = progressbar.Default(int64(totalRequests))

	var wg sync.WaitGroup
	stats := Stats{StatusCodes: make(map[int]int)}
	startTime := time.Now()

	requestsPerGoroutine := totalRequests / concurrency
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < requestsPerGoroutine; j++ {
				startReq := time.Now()
				// 根据 bodyfile 随机获取请求 URL 和 body，若为空则使用默认 URL 和空 body
				reqURL, body := getRandomRequest(url)
				// 根据 keepAliveRatio 随机决定是否使用 Keep-Alive
				useKeepAlive := rand.Float64() < keepAliveRatio
				client := createHTTPClient(useKeepAlive)
				success, duration, statusCode := sendRequest(client, reqURL, method, body)
				elapsed := time.Since(startReq)

				mu.Lock()
				stats.TotalRequests++
				if success {
					stats.SuccessRequests++
				} else {
					stats.FailedRequests++
				}
				stats.TotalTime += elapsed
				stats.ResponseTimes = append(stats.ResponseTimes, duration)
				stats.StatusCodes[statusCode]++

				bar.Add(1)

				// 每 reportInterval 个请求进行阶段性统计
				if stats.TotalRequests%int64(reportInterval) == 0 {
					reportStats(&stats, startTime)
				}
				mu.Unlock()
			}
		}()
	}

	wg.Wait()
	fmt.Println("\n======================================")
	fmt.Println("✅  测试完成！最终统计结果：")
	reportStats(&stats, startTime)

	// 防止历史数据为空导致折线图 panic
	ensureNonEmptyHistory()

	// 展示 TPS 和 QPS 的变化趋势
	fmt.Println("\n📈  TPS 变化趋势:")
	fmt.Println(asciigraph.Plot(tpsHistory, asciigraph.Height(10)))

	fmt.Println("\n📊  QPS 变化趋势:")
	fmt.Println(asciigraph.Plot(qpsHistory, asciigraph.Height(10)))

	// 展示 P50, P95, P99 的响应时间趋势
	fmt.Println("\n📉  响应时间趋势 (ms):")
	fmt.Println("P50:")
	fmt.Println(asciigraph.Plot(p50History, asciigraph.Height(5)))
	fmt.Println("P95:")
	fmt.Println(asciigraph.Plot(p95History, asciigraph.Height(5)))
	fmt.Println("P99:")
	fmt.Println(asciigraph.Plot(p99History, asciigraph.Height(5)))
}

// loadBodiesFromFile 读取 JSON 文件，支持两种格式：
// 1. ["body1", "body2", ...]
// 2. [["url1", "body1"], ["url2", "body2"], ...]
func loadBodiesFromFile(filename string) {
	data, err := ioutil.ReadFile(filename)
	if err != nil {
		fmt.Printf("❌ 无法读取 JSON 文件: %v\n", err)
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
			// 若只有 body，则 URL 为空，后续使用默认 URL
			requestBodies = append(requestBodies, []string{"", body})
		}
	}
}

// getRandomRequest 随机获取一个请求体
// 若 requestBodies 为空，则返回默认 URL 和空 body
// 若随机条目只有一个元素，则视为 body，返回默认 URL
// 否则返回条目的 URL 与 body
func getRandomRequest(defaultURL string) (string, string) {
	if len(requestBodies) == 0 {
		return defaultURL, ""
	}
	randomEntry := requestBodies[rand.Intn(len(requestBodies))]
	if len(randomEntry) == 1 {
		return defaultURL, randomEntry[0]
	}
	// 如果随机选中条目的 URL 为空，则使用默认 URL
	if randomEntry[0] == "" {
		return defaultURL, randomEntry[1]
	}
	return randomEntry[0], randomEntry[1]
}

// createHTTPClient 根据 keepAlive 参数创建 HTTP 客户端
func createHTTPClient(keepAlive bool) *http.Client {
	transport := &http.Transport{
		DisableKeepAlives: !keepAlive,
	}
	return &http.Client{
		Transport: transport,
		Timeout:   10 * time.Second,
	}
}

// sendRequest 发送 HTTP 请求，并返回是否成功、响应时间以及状态码
func sendRequest(client *http.Client, url, method, body string) (bool, time.Duration, int) {
	req, err := http.NewRequest(method, url, strings.NewReader(body))
	if err != nil {
		return false, 0, 0
	}

	req.Header.Set("User-Agent", "Go-HTTP-LoadTester")
	req.Header.Set("Content-Type", "application/json")

	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return false, 0, 0
	}
	defer resp.Body.Close()

	_, _ = ioutil.ReadAll(resp.Body)
	duration := time.Since(start)
	return resp.StatusCode >= 200 && resp.StatusCode < 300, duration, resp.StatusCode
}

// percentile 计算 durations 中指定百分比的响应时延
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

// reportStats 输出阶段性统计，并更新全局历史数组
func reportStats(stats *Stats, startTime time.Time) {
	totalDuration := time.Since(startTime)
	tps := float64(stats.SuccessRequests) / totalDuration.Seconds()
	qps := float64(stats.TotalRequests) / totalDuration.Seconds()

	// 对响应时延进行排序，计算 P50/P95/P99
	if len(stats.ResponseTimes) == 0 {
		fmt.Println("\n⚠️  没有足够的数据进行统计")
		return
	}
	sort.Slice(stats.ResponseTimes, func(i, j int) bool {
		return stats.ResponseTimes[i] < stats.ResponseTimes[j]
	})
	p50 := percentile(stats.ResponseTimes, 50)
	p95 := percentile(stats.ResponseTimes, 95)
	p99 := percentile(stats.ResponseTimes, 99)

	// 将阶段性指标保存到历史数组中（单位：ms）
	tpsHistory = append(tpsHistory, tps)
	qpsHistory = append(qpsHistory, qps)
	p50History = append(p50History, float64(p50.Milliseconds()))
	p95History = append(p95History, float64(p95.Milliseconds()))
	p99History = append(p99History, float64(p99.Milliseconds()))

	// 表格化输出当前统计
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

	// 输出 HTTP 状态码统计
	fmt.Println("\n📡  HTTP 状态码统计:")
	for code, count := range stats.StatusCodes {
		fmt.Printf("  - %d: %d 次\n", code, count)
	}
}

// ensureNonEmptyHistory 保证全局历史数组不为空，防止 asciigraph.Plot 时报空切片错误
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
