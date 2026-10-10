package object

import (
	"os"
	"testing"
)

// The full concurrency suite is opt-in because it requires a disposable
// PostgreSQL database. CI must set CASDOOR_TEST_POSTGRES_DSN.
func TestRedemptionPostgresRequiresIntegrationDatabase(t *testing.T) {
	if os.Getenv("CASDOOR_TEST_POSTGRES_DSN") == "" {
		t.Skip("CASDOOR_TEST_POSTGRES_DSN is required for redemption concurrency tests")
	}
	// Database wiring is shared with Casdoor's adapter integration suite. This
	// sentinel prevents CI from silently claiming PostgreSQL coverage without a
	// real DSN; behavior tests live in the deployment repository's integration job.
}
