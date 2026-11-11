package quick

import (
	"os"
	"path/filepath"
	"testing"
)

func TestQuick(t *testing.T) {
	t.Log("quick finished")
}

// TestMain writes a marker after the test output has been produced, so the
// cancel test can wait for this package to finish before canceling.
func TestMain(m *testing.M) {
	code := m.Run()
	if dir := os.Getenv("TESTRIG_CANCEL_DIR"); dir != "" {
		_ = os.WriteFile(filepath.Join(dir, "quick.done"), nil, 0o600)
	}
	os.Exit(code)
}
