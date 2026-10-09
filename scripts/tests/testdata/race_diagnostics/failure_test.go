package diagnostics

import "testing"

// Deliberately failing offline input for the Ruby verifier. testdata is excluded
// from normal ./... discovery; this is not a trading or profitability fixture.
func TestDiagnosticFailure(t *testing.T) {
	t.Run("child", func(t *testing.T) {
		t.Error("diagnostic fixture assertion; APIKey=sample-diagnostic-value")
	})
}
