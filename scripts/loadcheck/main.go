package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

type result struct {
	Name    string  `json:"name"`
	Method  string  `json:"method"`
	Path    string  `json:"path"`
	Count   int     `json:"count"`
	Workers int     `json:"workers"`
	OK      int     `json:"ok"`
	Errors  int     `json:"errors"`
	P50     float64 `json:"p50_ms"`
	P95     float64 `json:"p95_ms"`
	P99     float64 `json:"p99_ms"`
	Max     float64 `json:"max_ms"`
	RPS     float64 `json:"rps"`
	Elapsed float64 `json:"elapsed_s"`
}

func main() {
	base := os.Getenv("SED_BASE_URL")
	if base == "" {
		base = "http://localhost:8000"
	}
	client := &http.Client{
		Timeout: 15 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:        200,
			MaxIdleConnsPerHost: 200,
			IdleConnTimeout:     30 * time.Second,
		},
	}
	token := issue(client, base, "usr-org1-author")
	docID := createDoc(client, base, token)

	var report []result
	report = append(report,
		bench(client, "GET /v1/health", http.MethodGet, base+"/v1/health", "", nil, 2000, 50),
		bench(client, "GET /v1/profile", http.MethodGet, base+"/v1/profile", token, nil, 1000, 40),
		bench(client, "GET /v1/documents", http.MethodGet, base+"/v1/documents?limit=20", token, nil, 1000, 40),
		bench(client, "GET /v1/tasks/summary", http.MethodGet, base+"/v1/tasks/summary", token, nil, 1000, 40),
		bench(client, "GET /v1/documents/{id}", http.MethodGet, base+"/v1/documents/"+docID, token, nil, 800, 40),
		bench(client, "POST /dev/token", http.MethodPost, base+"/dev/token", "", []byte(`{"user_id":"usr-org1-author"}`), 1000, 40),
	)
	report = append(report, mixed(client, base, token, docID, 40, 8*time.Second))
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(report); err != nil {
		panic(err)
	}
}

func issue(client *http.Client, base, user string) string {
	body := []byte(`{"user_id":"` + user + `"}`)
	resp, err := client.Post(base+"/dev/token", "application/json", bytes.NewReader(body))
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var issued struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(raw, &issued); err != nil || issued.AccessToken == "" {
		panic(fmt.Sprintf("token: %s", raw))
	}
	return issued.AccessToken
}

func createDoc(client *http.Client, base, token string) string {
	req, _ := http.NewRequest(http.MethodPost, base+"/v1/documents", bytes.NewReader([]byte(`{"type_code":"служебная_записка","title":"Нагрузочная карточка","body":"Текст для чтения карточки под нагрузкой","addressee_user_id":"usr-org1-executor"}`)))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", fmt.Sprintf("load-%d", time.Now().UnixNano()))
	resp, err := client.Do(req)
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var doc struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil || doc.ID == "" {
		panic(fmt.Sprintf("create: %d %s", resp.StatusCode, raw))
	}
	return doc.ID
}

func bench(client *http.Client, name, method, url, token string, body []byte, count, workers int) result {
	var lat []time.Duration
	var mu sync.Mutex
	var ok, bad atomic.Int64
	jobs := make(chan int, count)
	for i := 0; i < count; i++ {
		jobs <- i
	}
	close(jobs)
	start := time.Now()
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range jobs {
				began := time.Now()
				status := do(client, method, url, token, body)
				elapsed := time.Since(began)
				mu.Lock()
				lat = append(lat, elapsed)
				mu.Unlock()
				if status >= 200 && status < 300 {
					ok.Add(1)
				} else {
					bad.Add(1)
				}
			}
		}()
	}
	wg.Wait()
	total := time.Since(start)
	return summarize(name, method, pathOf(url), count, workers, int(ok.Load()), int(bad.Load()), lat, total)
}

func mixed(client *http.Client, base, token, docID string, workers int, dur time.Duration) result {
	targets := []struct {
		method, url, token string
	}{
		{http.MethodGet, base + "/v1/health", ""},
		{http.MethodGet, base + "/v1/profile", token},
		{http.MethodGet, base + "/v1/documents?limit=20", token},
		{http.MethodGet, base + "/v1/tasks/summary", token},
		{http.MethodGet, base + "/v1/documents/" + docID, token},
	}
	var lat []time.Duration
	var mu sync.Mutex
	var ok, bad atomic.Int64
	stop := time.Now().Add(dur)
	start := time.Now()
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(seed int) {
			defer wg.Done()
			n := seed
			for time.Now().Before(stop) {
				target := targets[n%len(targets)]
				n++
				began := time.Now()
				status := do(client, target.method, target.url, target.token, nil)
				elapsed := time.Since(began)
				mu.Lock()
				lat = append(lat, elapsed)
				mu.Unlock()
				if status >= 200 && status < 300 {
					ok.Add(1)
				} else {
					bad.Add(1)
				}
			}
		}(i)
	}
	wg.Wait()
	total := time.Since(start)
	return summarize("смесь 8 с", "GET", "health+profile+реестр+сводка+карточка", len(lat), workers, int(ok.Load()), int(bad.Load()), lat, total)
}

func do(client *http.Client, method, url, token string, body []byte) int {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, url, reader)
	if err != nil {
		return 0
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	return resp.StatusCode
}

func summarize(name, method, path string, count, workers, ok, bad int, lat []time.Duration, total time.Duration) result {
	sort.Slice(lat, func(i, j int) bool { return lat[i] < lat[j] })
	pick := func(q float64) float64 {
		if len(lat) == 0 {
			return 0
		}
		idx := int(float64(len(lat)-1) * q)
		return float64(lat[idx].Microseconds()) / 1000
	}
	elapsed := total.Seconds()
	rps := 0.0
	if elapsed > 0 {
		rps = float64(len(lat)) / elapsed
	}
	return result{
		Name: name, Method: method, Path: path, Count: count, Workers: workers,
		OK: ok, Errors: bad, P50: round(pick(0.50)), P95: round(pick(0.95)), P99: round(pick(0.99)),
		Max: round(pick(1)), RPS: round(rps), Elapsed: round(elapsed),
	}
}

func round(v float64) float64 {
	return float64(int(v*100+0.5)) / 100
}

func pathOf(url string) string {
	if i := bytes.Index([]byte(url), []byte("/v1/")); i >= 0 {
		return url[i:]
	}
	if i := bytes.Index([]byte(url), []byte("/dev/")); i >= 0 {
		return url[i:]
	}
	return url
}
