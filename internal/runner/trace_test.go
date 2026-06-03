package runner

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTraceGeneration(t *testing.T) {
	t.Parallel()

	// 1. Arrange: Provide simulated go test -json output with precise Timestamps
	input := `{"Time":"2026-06-03T12:00:00.000000Z","Action":"start","Package":"pkg1"}
{"Time":"2026-06-03T12:00:00.100000Z","Action":"run","Package":"pkg1","Test":"TestA"}
{"Time":"2026-06-03T12:00:00.200000Z","Action":"pass","Package":"pkg1","Test":"TestA","Elapsed":0.1}
{"Time":"2026-06-03T12:00:00.300000Z","Action":"pass","Package":"pkg1","Elapsed":0.3}
`

	// 2. Act: Parse using a new function or helper that handles tracing
	events, err := parseTraceEvents(strings.NewReader(input), 0)
	require.NoError(t, err)

	// 3. Assert: Verify the trace events generated match expectations
	assert.NotEmpty(t, events)

	var pkgEvent, testEvent *TraceEvent
	for i := range events {
		ev := &events[i]
		if ev.Ph == "X" {
			switch ev.Name {
			case "pkg1":
				pkgEvent = ev
			case "TestA":
				testEvent = ev
			}
		}
	}

	require.NotNil(t, pkgEvent, "should have pkg1 trace event")
	require.NotNil(t, testEvent, "should have TestA trace event")

	// Timestamps are in microseconds.
	// pkg1 started at 12:00:00.000000Z, ended at 12:00:00.300000Z -> 300,000 microseconds
	// TestA started at 12:00:00.100000Z, ended at 12:00:00.200000Z -> 100,000 microseconds
	assert.Equal(t, int64(300000), pkgEvent.Dur)
	assert.Equal(t, int64(100000), testEvent.Dur)

	assert.Greater(t, testEvent.Ts, pkgEvent.Ts, "Test should start after package")
}

func TestTraceGeneration_TableDriven(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		input          string
		expectedEvents func(t *testing.T, events []TraceEvent)
	}{
		{
			name: "incomplete events (timeout)",
			input: `{"Time":"2026-06-03T12:00:00.000000Z","Action":"start","Package":"pkg1"}
{"Time":"2026-06-03T12:00:00.100000Z","Action":"run","Package":"pkg1","Test":"TestA"}
`,
			expectedEvents: func(t *testing.T, events []TraceEvent) {
				var pkgEvent, testEvent *TraceEvent
				for i := range events {
					ev := &events[i]
					if ev.Ph == "X" {
						switch ev.Name {
						case "pkg1":
							pkgEvent = ev
						case "TestA":
							testEvent = ev
						}
					}
				}
				require.NotNil(t, pkgEvent)
				require.NotNil(t, testEvent)
				// They both ended at last seen time (12:00:00.100000Z)
				assert.Equal(t, int64(100000), pkgEvent.Dur)
				assert.Equal(t, int64(0), testEvent.Dur)
			},
		},
		{
			name:  "missing start event (using elapsed fallback)",
			input: `{"Time":"2026-06-03T12:00:00.200000Z","Action":"pass","Package":"pkg1","Test":"TestA","Elapsed":0.15}`,
			expectedEvents: func(t *testing.T, events []TraceEvent) {
				var testEvent *TraceEvent
				for i := range events {
					ev := &events[i]
					if ev.Ph == "X" && ev.Name == "TestA" {
						testEvent = ev
					}
				}
				require.NotNil(t, testEvent)
				assert.Equal(t, int64(150000), testEvent.Dur)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			events, err := parseTraceEvents(strings.NewReader(tc.input), 0)
			require.NoError(t, err)
			tc.expectedEvents(t, events)
		})
	}
}

func TestWriteTrace(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	// Write simulated log files
	log1 := `{"Time":"2026-06-03T12:00:00.000000Z","Action":"start","Package":"pkg1"}
{"Time":"2026-06-03T12:00:00.300000Z","Action":"pass","Package":"pkg1","Elapsed":0.3}
`
	log2 := `{"Time":"2026-06-03T12:01:00.000000Z","Action":"start","Package":"pkg1"}
{"Time":"2026-06-03T12:01:00.200000Z","Action":"pass","Package":"pkg1","Elapsed":0.2}
`

	err := os.WriteFile(filepath.Join(tmpDir, "iteration-0.log.jsonl"), []byte(log1), 0600)
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(tmpDir, "iteration-1.log.jsonl"), []byte(log2), 0600)
	require.NoError(t, err)

	report := &Report{
		IterationSummaries: []IterationSummary{
			{Index: 0, ShuffleSeed: 123},
			{Index: 1, ShuffleSeed: 456},
		},
	}

	err = WriteTrace(tmpDir, report)
	require.NoError(t, err)

	// Verify trace.json exists and is valid json
	tracePath := filepath.Join(tmpDir, "trace.json")
	assert.FileExists(t, tracePath)

	content, err := os.ReadFile(tracePath) //nolint:gosec // G304: path from filepath.Join
	require.NoError(t, err)

	var traceEvents []TraceEvent
	err = json.Unmarshal(content, &traceEvents)
	require.NoError(t, err)

	// We expect metadata and complete events
	assert.NotEmpty(t, traceEvents)
}

func TestServeTrace(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	// Write empty trace
	err := os.WriteFile(filepath.Join(tmpDir, "trace.json"), []byte("[]"), 0600)
	require.NoError(t, err)

	ctx := t.Context()

	var mu sync.Mutex
	var browserURL string
	openBrowserMock := func(url string) error {
		mu.Lock()
		browserURL = url
		mu.Unlock()
		return nil
	}

	done := make(chan error, 1)
	go func() {
		// Run ServeTrace and record outcome
		done <- ServeTrace(ctx, tmpDir, nil, "127.0.0.1:0", openBrowserMock)
	}()

	// Wait for browser URL to be set
	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return browserURL != ""
	}, 1*time.Second, 10*time.Millisecond)

	mu.Lock()
	targetURLRaw := browserURL
	mu.Unlock()

	idx := strings.Index(targetURLRaw, "url=")
	require.Greater(t, idx, -1)
	targetURL := targetURLRaw[idx+4:]

	// Query with good origin to trigger the auto-exit
	req, err := http.NewRequestWithContext(ctx, "GET", targetURL, nil)
	require.NoError(t, err)
	req.Header.Set("Origin", "https://ui.perfetto.dev")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "https://ui.perfetto.dev", resp.Header.Get("Access-Control-Allow-Origin"))
	_ = resp.Body.Close()

	// Verify that ServeTrace auto-exits on its own after serving
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("ServeTrace did not auto-exit after serving trace")
	}
}

func TestTraceGeneration_LargeLine(t *testing.T) {
	t.Parallel()
	largeOutput := strings.Repeat("A", 1024*1024)
	input := `{"Time":"2026-06-03T12:00:00.000000Z","Action":"output","Package":"pkg1","Test":"TestA","Output":"` + largeOutput + `"}` + "\n"

	events, err := parseTraceEvents(strings.NewReader(input), 0)
	require.NoError(t, err)
	for _, ev := range events {
		assert.NotEqual(t, "X", ev.Ph, "Should not produce a duration event for output action")
	}
}

func TestWriteTrace_EmptyMatches(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	err := WriteTrace(tmpDir, nil)
	require.NoError(t, err)

	content, err := os.ReadFile(filepath.Join(tmpDir, "trace.json")) //nolint:gosec // G304: path from filepath.Join
	require.NoError(t, err)
	assert.Equal(t, "[]", strings.TrimSpace(string(content)))
}

func TestWriteTrace_InvalidIterationName(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	err := os.WriteFile(filepath.Join(tmpDir, "iteration-invalid.log.jsonl"), []byte("{}"), 0600)
	require.NoError(t, err)

	err = WriteTrace(tmpDir, &Report{})
	require.NoError(t, err)
}
