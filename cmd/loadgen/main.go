// Command loadgen replaces the shell "make load" scripts a demo like this
// would normally have. It drives the running Order Service with normal,
// slow, error, db-fail, or mixed/concurrent traffic so there is something
// interesting to look at in SigNoz. Wired as Mage targets in magefile.go
// (loadgen:Normal, loadgen:Slow, loadgen:Errors, loadgen:Concurrent).
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"math/rand"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

var items = []string{"widget", "gadget", "gizmo", "doohickey", "thingamajig"}

type orderPayload struct {
	CustomerName string `json:"customer_name"`
	Item         string `json:"item"`
	Quantity     int    `json:"quantity"`
	AmountCents  int64  `json:"amount_cents"`
}

func main() {
	var (
		baseURL     = flag.String("base-url", envOr("LOADGEN_BASE_URL", "http://localhost:8090"), "Order Service base URL")
		scenario    = flag.String("scenario", "normal", "normal | slow | error | db-fail | mixed")
		count       = flag.Int("count", 10, "number of requests to send")
		concurrency = flag.Int("concurrency", 1, "number of concurrent workers")
		timeout     = flag.Duration("timeout", 10*time.Second, "per-request client timeout")
	)
	flag.Parse()

	client := &http.Client{Timeout: *timeout}

	var ok, failed int64
	var wg sync.WaitGroup
	jobs := make(chan int, *count)
	for i := 0; i < *count; i++ {
		jobs <- i
	}
	close(jobs)

	start := time.Now()
	for w := 0; w < *concurrency; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				s := pickScenario(*scenario, i)
				status, err := sendOrder(client, *baseURL, s)
				if err != nil || status >= 400 {
					atomic.AddInt64(&failed, 1)
					fmt.Printf("[%s] request %d -> status=%d err=%v\n", s, i, status, err)
				} else {
					atomic.AddInt64(&ok, 1)
					fmt.Printf("[%s] request %d -> status=%d\n", s, i, status)
				}
			}
		}()
	}
	wg.Wait()

	fmt.Printf("\ndone in %s — ok=%d failed=%d total=%d\n", time.Since(start).Round(time.Millisecond), ok, failed, *count)
}

// pickScenario resolves "mixed" into a rotating set of scenarios so a single
// `loadgen -scenario=mixed` run produces normal, slow, and error traces
// side by side — useful for demonstrating the contrast in SigNoz.
func pickScenario(requested string, i int) string {
	if requested != "mixed" {
		return requested
	}
	rotation := []string{"normal", "normal", "slow", "error", "db-fail"}
	return rotation[i%len(rotation)]
}

func sendOrder(client *http.Client, baseURL, scenario string) (int, error) {
	payload := orderPayload{
		CustomerName: fmt.Sprintf("customer-%d", rand.Intn(1000)),
		Item:         items[rand.Intn(len(items))],
		Quantity:     1 + rand.Intn(5),
		AmountCents:  int64(500 + rand.Intn(9500)),
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return 0, err
	}

	req, err := http.NewRequest(http.MethodPost, baseURL+"/api/v1/orders", bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	if scenario != "" && scenario != "normal" {
		req.Header.Set("X-Demo-Scenario", scenario)
	}

	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	return resp.StatusCode, nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
