// Command loadgen replaces the shell "make load" scripts a demo like this
// would normally have. It drives the running Order Service with normal,
// slow, error, db-fail, mixed/concurrent, or a full end-to-end traffic
// scenario so there is something interesting to look at in SigNoz.
//
// Wired as Mage targets in magefile.go:
//   mage loadgen:Normal      – 20 sequential healthy requests
//   mage loadgen:Slow        – requests that hit injected SQLite latency
//   mage loadgen:Errors      – simulated app + db failures
//   mage loadgen:Concurrent  – 10-worker burst of mixed traffic
//   mage loadgen:Full        – all of the above in one shot (populates every dashboard panel)
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
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
		scenario    = flag.String("scenario", "normal", "normal | slow | error | db-fail | mixed | list | get | full")
		count       = flag.Int("count", 10, "number of requests to send (ignored for 'full')")
		concurrency = flag.Int("concurrency", 1, "number of concurrent workers")
		timeout     = flag.Duration("timeout", 10*time.Second, "per-request client timeout")
	)
	flag.Parse()

	client := &http.Client{Timeout: *timeout}

	if *scenario == "full" {
		runFull(client, *baseURL)
		return
	}

	if *scenario == "list" {
		runListOrders(client, *baseURL, *count)
		return
	}

	if *scenario == "get" {
		runGetByID(client, *baseURL, *count)
		return
	}

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

// runFull executes every traffic scenario in sequence so a single
// `mage loadgen:Full` (or `loadgen -scenario=full`) populates every
// panel in the SigNoz dashboard without the user having to run each
// scenario individually:
//
//  1. 30 normal orders          → orders_created_total, db_calls_total, db_rows_affected_total
//  2. 10 slow orders            → db_operation_duration_seconds P95 spike
//  3. 10 app-error orders       → order_errors_total, error_rate panel
//  4. 10 db-fail orders         → db_errors_total, db_calls_total status=error
//  5. 20 list-orders GETs       → db_rows_returned_total (SELECT * N rows)
//  6. 10 get-by-id GETs         → db_rows_returned_total (SELECT single row)
//  7. 60-req concurrent burst   → RPS spike, p50/p95 latency, mixed error rate
func runFull(client *http.Client, baseURL string) {
	fmt.Println("══════════════════════════════════════════")
	fmt.Println("  loadgen:full — populating all dashboard panels")
	fmt.Println("══════════════════════════════════════════")

	// ── 1. Normal orders ────────────────────────────────────────────────────
	fmt.Println("\n[1/7] Sending 30 normal orders …")
	runScenarioBatch(client, baseURL, "normal", 30, 1)

	// ── 2. Slow orders ──────────────────────────────────────────────────────
	fmt.Println("\n[2/7] Sending 10 slow orders (1.8 s DB sleep each) …")
	runScenarioBatch(client, baseURL, "slow", 10, 1)

	// ── 3. App errors ───────────────────────────────────────────────────────
	fmt.Println("\n[3/7] Sending 10 application-error orders …")
	runScenarioBatch(client, baseURL, "error", 10, 1)

	// ── 4. DB-fail errors ───────────────────────────────────────────────────
	fmt.Println("\n[4/7] Sending 10 db-fail orders …")
	runScenarioBatch(client, baseURL, "db-fail", 10, 1)

	// ── 5. List all orders ──────────────────────────────────────────────────
	fmt.Println("\n[5/7] Listing all orders 20 times (SELECT * rows) …")
	runListOrders(client, baseURL, 20)

	// ── 6. Get individual orders by ID ──────────────────────────────────────
	fmt.Println("\n[6/7] Fetching individual orders by ID (10 lookups) …")
	runGetByID(client, baseURL, 10)

	// ── 7. Concurrent mixed burst ───────────────────────────────────────────
	fmt.Println("\n[7/7] Firing 60-request concurrent burst (10 workers, mixed) …")
	runScenarioBatch(client, baseURL, "mixed", 60, 10)

	fmt.Println("\n══════════════════════════════════════════")
	fmt.Println("  loadgen:full complete — open SigNoz dashboard")
	fmt.Println("══════════════════════════════════════════")
}

// runScenarioBatch sends `count` requests using `concurrency` workers.
func runScenarioBatch(client *http.Client, baseURL, scenario string, count, concurrency int) {
	var ok, failed int64
	var wg sync.WaitGroup
	jobs := make(chan int, count)
	for i := 0; i < count; i++ {
		jobs <- i
	}
	close(jobs)

	start := time.Now()
	for w := 0; w < concurrency; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				s := pickScenario(scenario, i)
				status, err := sendOrder(client, baseURL, s)
				if err != nil || status >= 400 {
					atomic.AddInt64(&failed, 1)
					fmt.Printf("  [%s] req %d -> %d err=%v\n", s, i, status, err)
				} else {
					atomic.AddInt64(&ok, 1)
					fmt.Printf("  [%s] req %d -> %d\n", s, i, status)
				}
			}
		}()
	}
	wg.Wait()
	fmt.Printf("  → done in %s  ok=%d failed=%d\n", time.Since(start).Round(time.Millisecond), ok, failed)
}

// runListOrders fetches GET /api/v1/orders `count` times to exercise
// the SELECT * code path and populate db_rows_returned_total.
func runListOrders(client *http.Client, baseURL string, count int) {
	start := time.Now()
	ok := 0
	for i := 0; i < count; i++ {
		resp, err := client.Get(baseURL + "/api/v1/orders")
		if err != nil {
			fmt.Printf("  [list] req %d -> err=%v\n", i, err)
			continue
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		fmt.Printf("  [list] req %d -> %d\n", i, resp.StatusCode)
		ok++
	}
	fmt.Printf("  → done in %s  ok=%d total=%d\n", time.Since(start).Round(time.Millisecond), ok, count)
}

// runGetByID fetches the first `count` orders by ID from the list endpoint,
// then calls GET /api/v1/orders/:id on each to exercise the single-row SELECT
// path and populate db_rows_returned_total.
func runGetByID(client *http.Client, baseURL string, count int) {
	// Fetch list to collect IDs
	resp, err := client.Get(baseURL + "/api/v1/orders")
	if err != nil {
		fmt.Printf("  [get-by-id] failed to list orders: %v\n", err)
		return
	}
	defer resp.Body.Close()

	var orders []struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&orders); err != nil {
		fmt.Printf("  [get-by-id] failed to decode orders: %v\n", err)
		return
	}

	if len(orders) == 0 {
		fmt.Println("  [get-by-id] no orders found, skipping")
		return
	}

	start := time.Now()
	ok := 0
	for i := 0; i < count; i++ {
		order := orders[i%len(orders)]
		r, err := client.Get(baseURL + "/api/v1/orders/" + order.ID)
		if err != nil {
			fmt.Printf("  [get] req %d id=%s -> err=%v\n", i, order.ID, err)
			continue
		}
		io.Copy(io.Discard, r.Body)
		r.Body.Close()
		fmt.Printf("  [get] req %d id=%s -> %d\n", i, order.ID[:8], r.StatusCode)
		ok++
	}
	fmt.Printf("  → done in %s  ok=%d total=%d\n", time.Since(start).Round(time.Millisecond), ok, count)
}

// pickScenario resolves "mixed" into a rotating set of scenarios so a single
// run produces normal, slow, and error traces side by side — useful for
// demonstrating the contrast in SigNoz.
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
