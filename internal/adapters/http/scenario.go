package http

import (
	"net/http"

	"github.com/vinaycharlie01/signoz-demo/internal/demoscenario"
)

// scenarioHeader is how the load generator (cmd/loadgen) or a curl call
// asks this service to deliberately misbehave. See internal/demoscenario
// for the supported values and internal/adapters/sqlite for where "slow"
// and "db-fail" actually take effect.
const scenarioHeader = "X-Demo-Scenario"

// withScenario is middleware that reads scenarioHeader (if present) and
// stores it on the request context for the use case / repository layers to
// read back via demoscenario.From.
func withScenario(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		scenario := demoscenario.Parse(r.Header.Get(scenarioHeader))
		if scenario != demoscenario.None {
			r = r.WithContext(demoscenario.Into(r.Context(), scenario))
		}
		next.ServeHTTP(w, r)
	})
}
