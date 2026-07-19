package main

import "fmt"

// This repo's build/dev commands run through Mage, not this binary — see
// magefile.go. Run `mage -l` to list every target (go:build, docker:up,
// loadgen:normal, ...). The actual service entry point is cmd/api.
func main() {
	fmt.Println("signoz-demo build tooling is Mage-driven. Run `mage -l` to list targets.")
}
