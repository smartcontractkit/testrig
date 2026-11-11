package slow

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestSlow(t *testing.T) {
	if dir := os.Getenv("TESTRIG_CANCEL_DIR"); dir != "" {
		pid := strconv.Itoa(os.Getpid())
		_ = os.WriteFile(filepath.Join(dir, "slow.pid"), []byte(pid), 0o600)
	}
	time.Sleep(30 * time.Second)
}
