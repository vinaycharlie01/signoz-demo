//go:build mage

// This repo intentionally has no Makefile and no .sh scripts. Every command
// this project needs is a Mage target, built on nirantaraai/nava's typed
// runners (github.com/nirantaraai/nava) — the same tool this org's other
// repos (e.g. nirantaraai/nava itself) use for build automation. Options
// live in go.yaml / docker.yaml / loadgen-*.yaml, not hardcoded in Go.
//
// Usage:
//
//	go install github.com/magefile/mage@latest
//	mage -l               # list every target
//	mage go:build          # build ./cmd/api -> bin/api
//	mage docker:up         # docker compose up --build -d
//	mage loadgen:normal    # send 20 healthy requests
package main

import (
	"fmt"

	"github.com/magefile/mage/mg"

	dockermagex "github.com/nirantaraai/nava/mage/docker"
	gomagex "github.com/nirantaraai/nava/mage/golang"
)

func init() {
	_ = gomagex.LoadConfig("go.yaml")
	_ = dockermagex.LoadConfig("docker.yaml")
}

// Go namespace: local Go developer workflow (setup, build, run, test).
type Go mg.Namespace

// Setup downloads and tidies Go module dependencies.
func (Go) Setup() error { return gomagex.Setup() }

// Build compiles cmd/api into bin/api (CGO disabled, per go.yaml).
func (Go) Build() error { return gomagex.Build() }

// Run runs cmd/api locally with `go run` (Ctrl+C stops it gracefully).
func (Go) Run() error { return gomagex.Run() }

// Test runs the full unit test suite.
func (Go) Test() error { return gomagex.Test() }

// Vet runs `go vet ./...`.
func (Go) Vet() error { return gomagex.Vet() }

// Docker namespace: the local SigNoz-backend + app stack (docker-compose.yml).
type Docker mg.Namespace

// Up builds and starts ClickHouse + the SigNoz OTel Collector + the app,
// detached. See docker-compose.yml's header comment for what it does and
// does not provision (the SigNoz app/UI itself is installed separately via
// Foundry — see README.md).
func (Docker) Up() error { return dockermagex.ComposeUp() }

// Down stops and removes the stack's containers (data volumes are kept).
func (Docker) Down() error { return dockermagex.ComposeDown() }

// Build rebuilds the app image without starting anything.
func (Docker) Build() error { return dockermagex.ComposeBuild() }

// Loadgen namespace: cmd/loadgen scenarios, each driven by its own
// loadgen-*.yaml — see Phase 5 of the project spec ("Generate Interesting
// Production Scenarios"). Run `mage docker:up` (or `mage go:run` locally)
// first so there's a service listening.
type Loadgen mg.Namespace

// Normal sends 20 sequential, healthy requests.
func (Loadgen) Normal() error { return runLoadgen("loadgen-normal.yaml") }

// Slow sends requests that hit the injected SQLite latency.
func (Loadgen) Slow() error { return runLoadgen("loadgen-slow.yaml") }

// Errors sends a mix of use-case and repository-level simulated failures.
func (Loadgen) Errors() error { return runLoadgen("loadgen-errors.yaml") }

// Concurrent sends a 10-worker burst of mixed traffic.
func (Loadgen) Concurrent() error { return runLoadgen("loadgen-concurrent.yaml") }

func runLoadgen(configPath string) error {
	runner, err := gomagex.NewRunnerFromYAML(configPath)
	if err != nil {
		return fmt.Errorf("load %s: %w", configPath, err)
	}
	return runner.RunFromConfig()
}
