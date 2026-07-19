// Package demoscenario lets the load generator (or any caller) ask this
// service to deliberately misbehave in a specific, observable way, so the
// resulting telemetry has more to show in SigNoz than "everything is
// healthy". It is test/demo-only scaffolding, not a domain concept — it
// never touches internal/domain.
package demoscenario

import "context"

// Scenario is requested via the X-Demo-Scenario HTTP header (see
// internal/adapters/http/scenario.go) and read back out in the use case
// and repository layers via context.
type Scenario string

const (
	None   Scenario = ""
	Slow   Scenario = "slow"    // adds artificial latency to the DB write
	Error  Scenario = "error"   // fails fast in the use case, before touching the DB
	DBFail Scenario = "db-fail" // fails inside the repository, simulating a DB outage
)

// Parse maps the raw header value to a known Scenario, defaulting to None
// for anything unrecognized so a typo never silently changes behavior.
func Parse(raw string) Scenario {
	switch Scenario(raw) {
	case Slow, Error, DBFail:
		return Scenario(raw)
	default:
		return None
	}
}

type ctxKey struct{}

// Into stores the requested Scenario on ctx.
func Into(ctx context.Context, s Scenario) context.Context {
	return context.WithValue(ctx, ctxKey{}, s)
}

// From reads the Scenario stored on ctx, defaulting to None.
func From(ctx context.Context) Scenario {
	s, _ := ctx.Value(ctxKey{}).(Scenario)
	return s
}
