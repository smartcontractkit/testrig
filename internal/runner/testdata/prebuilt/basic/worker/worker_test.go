package worker

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestSeesWorkerEnv proves that a prebuilt binary receives the environment of
// the diagnose worker that ran it. With TESTRIG_WORKER_EXPECT > 1 it also waits
// until that many distinct workers have started, so a parallel test can prove
// every worker ran concurrently.
func TestSeesWorkerEnv(t *testing.T) {
	value := os.Getenv("TESTRIG_WORKER")
	if value == "" {
		t.Fatal("TESTRIG_WORKER is not set")
	}
	dir := os.Getenv("TESTRIG_WORKER_DIR")
	if dir == "" {
		t.Fatal("TESTRIG_WORKER_DIR is not set")
	}

	path := filepath.Join(dir, "workers")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(value + "\n"); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	expect, _ := strconv.Atoi(os.Getenv("TESTRIG_WORKER_EXPECT"))
	if expect <= 1 {
		return
	}

	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if len(distinctWorkers(path)) >= expect {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("only saw workers %v, want %d distinct", distinctWorkers(path), expect)
}

func distinctWorkers(path string) []string {
	data, err := os.ReadFile(path) //nolint:gosec // G304: fixture path from the test environment
	if err != nil {
		return nil
	}
	var workers []string
	for _, value := range strings.Split(string(data), "\n") {
		if value != "" && !slices.Contains(workers, value) {
			workers = append(workers, value)
		}
	}
	return workers
}
