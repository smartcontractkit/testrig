package runner

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/smartcontractkit/testrig/internal/output"
)

func TestTraceGeneration(t *testing.T) {
	t.Parallel()

	// 1. Arrange: Provide simulated go test -json output with precise Timestamps
	input := `{"Time":"2026-06-03T12:00:00.000000Z","Action":"start","Package":"pkg1"}
{"Time":"2026-06-03T12:00:00.100000Z","Action":"run","Package":"pkg1","Test":"TestA"}
{"Time":"2026-06-03T12:00:00.200000Z","Action":"pass","Package":"pkg1","Test":"TestA","Elapsed":0.1}
{"Time":"2026-06-03T12:00:00.300000Z","Action":"pass","Package":"pkg1","Elapsed":0.3}
`

	events, err := parseTraceEvents(strings.NewReader(input), 0, nil, make(map[string]int))
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
			events, err := parseTraceEvents(strings.NewReader(tc.input), 0, nil, make(map[string]int))
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

	// 1. Setup: Create sample JSONL file and write traces
	// This ensures we test WriteTrace's parsing of iterations
	var traceFiles []string
	traceFiles, err = WriteTrace(nil, tmpDir)
	require.NoError(t, err)
	require.Len(t, traceFiles, 2, "expected two trace files to be written")

	// Verify trace-1.json exists and is valid json
	tracePath := filepath.Join(tmpDir, traceFiles[0])
	assert.FileExists(t, tracePath)

	// Read trace-1.json
	content1, err := os.ReadFile(filepath.Join(tmpDir, traceFiles[0])) //nolint:gosec // G304: path from filepath.Join
	require.NoError(t, err)
	var traceEvents1 []TraceEvent
	err = json.Unmarshal(content1, &traceEvents1)
	require.NoError(t, err)

	// Read trace-2.json
	content2, err := os.ReadFile(filepath.Join(tmpDir, traceFiles[1])) //nolint:gosec // G304: path from filepath.Join
	require.NoError(t, err)
	var traceEvents2 []TraceEvent
	err = json.Unmarshal(content2, &traceEvents2)
	require.NoError(t, err)

	// Verify specific process name metadata and duration events in Iteration 1
	var hasProc1, hasThreadName, hasPkg1Event bool
	for _, ev := range traceEvents1 {
		if ev.Name == "process_name" && ev.Pid == 1 && ev.Args["name"] == "pkg1" {
			hasProc1 = true
		}
		if ev.Name == "thread_name" && ev.Pid == 1 && ev.Tid == 0 && ev.Args["name"] == "Main" {
			hasThreadName = true
		}
		if ev.Name == "pkg1" && ev.Ph == "X" {
			if ev.Pid == 1 && ev.Dur == 300000 && ev.Tid == 0 {
				hasPkg1Event = true
			}
		}
	}
	assert.True(t, hasProc1, "should have process name for pkg1")
	assert.True(t, hasThreadName, "should have thread name for Main")
	assert.True(t, hasPkg1Event, "should have pkg1 event in Iteration 1 (300ms)")

	// Verify specific events in Iteration 2
	var hasPkg2Event bool
	for _, ev := range traceEvents2 {
		if ev.Name == "pkg1" && ev.Ph == "X" {
			if ev.Pid == 1 && ev.Dur == 200000 && ev.Tid == 0 {
				hasPkg2Event = true
			}
		}
	}
	assert.True(t, hasPkg2Event, "should have pkg1 event in Iteration 2 (200ms)")
}

func TestServeTrace(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	// Write empty trace
	err := os.WriteFile(filepath.Join(tmpDir, "trace-1.json"), []byte("[]"), 0600)
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(tmpDir, "trace-2.json"), []byte("[]"), 0600)
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
		done <- ServeTrace(ctx, tmpDir, []string{"trace-1.json", "trace-2.json"}, nil, TraceServeOptions{
			Addr:        "127.0.0.1:0",
			OpenBrowser: openBrowserMock,
		})
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

	// Query both files to trigger auto-exit
	req, err := http.NewRequestWithContext(ctx, "GET", targetURL, nil)
	require.NoError(t, err)
	req.Header.Set("Origin", "https://ui.perfetto.dev")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "https://ui.perfetto.dev", resp.Header.Get("Access-Control-Allow-Origin"))
	_ = resp.Body.Close()

	// Query second file
	secondURL := strings.Replace(targetURL, "trace-2.json", "trace-1.json", 1)
	req2, err := http.NewRequestWithContext(ctx, "GET", secondURL, nil)
	require.NoError(t, err)
	req2.Header.Set("Origin", "https://ui.perfetto.dev")

	resp2, err := http.DefaultClient.Do(req2)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp2.StatusCode)
	_ = resp2.Body.Close()

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
	events, err := parseTraceEvents(strings.NewReader(input), 0, nil, make(map[string]int))
	require.NoError(t, err)
	for _, ev := range events {
		assert.NotEqual(t, "X", ev.Ph, "Should not produce a duration event for output action")
	}
}

func TestWriteTrace_EmptyMatches(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	var stderr strings.Builder
	out := output.NewForTest(false, io.Discard, &stderr, false)

	files, err := WriteTrace(out, tmpDir)
	require.NoError(t, err)
	assert.Empty(t, files)

	assert.Contains(t, stderr.String(), "warning: no test execution events recorded in trace")
}

func TestWriteTrace_InvalidIterationName(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	err := os.WriteFile(filepath.Join(tmpDir, "iteration-invalid.log.jsonl"), []byte("{}"), 0600)
	require.NoError(t, err)

	files, err := WriteTrace(nil, tmpDir)
	require.NoError(t, err)
	assert.Empty(t, files)
}

func TestParseTraceEvents_MalformedJSONL(t *testing.T) {
	t.Parallel()

	var stderr strings.Builder
	out := output.NewForTest(false, io.Discard, &stderr, false)
	input := "{not valid json}\n" +
		`{"Time":"2026-06-03T12:00:00.000000Z","Action":"pass","Package":"pkg1","Elapsed":0.1}` + "\n"

	events, err := parseTraceEvents(strings.NewReader(input), 2, out, make(map[string]int))
	require.NoError(t, err)
	assert.Contains(t, stderr.String(), "trace: skip malformed jsonl (iteration 2)")
	assert.NotEmpty(t, events)
}

func TestTraceViewerEnabled(t *testing.T) {
	t.Setenv("TESTRIG_NO_BROWSER", "1")
	t.Setenv("CI", "")
	assert.False(t, traceViewerEnabled())

	t.Setenv("TESTRIG_NO_BROWSER", "")
	t.Setenv("CI", "true")
	assert.False(t, traceViewerEnabled())
}

func TestSafeStringArg(t *testing.T) {
	t.Parallel()
	assert.Empty(t, safeStringArg(nil, "package"))
	assert.Empty(t, safeStringArg(map[string]any{}, "package"))
	assert.Empty(t, safeStringArg(map[string]any{"package": 123}, "package"))
	assert.Equal(t, "pkg", safeStringArg(map[string]any{"package": "pkg"}, "package"))
}
