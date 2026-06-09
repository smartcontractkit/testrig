package runner

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/smartcontractkit/testrig/internal/output"
)

func TestTraceGeneration_NoOverlappingSlicesPerTrack(t *testing.T) {
	t.Parallel()

	// Parallel tests share one package track via nestable async slices, not X events.
	input := `{"Time":"2026-06-03T12:00:00.000000Z","Action":"start","Package":"pkg1"}
{"Time":"2026-06-03T12:00:00.100000Z","Action":"run","Package":"pkg1","Test":"TestA"}
{"Time":"2026-06-03T12:00:00.150000Z","Action":"run","Package":"pkg1","Test":"TestB"}
{"Time":"2026-06-03T12:00:00.200000Z","Action":"pass","Package":"pkg1","Test":"TestA","Elapsed":0.1}
{"Time":"2026-06-03T12:00:00.250000Z","Action":"pass","Package":"pkg1","Test":"TestB","Elapsed":0.1}
{"Time":"2026-06-03T12:00:00.300000Z","Action":"pass","Package":"pkg1","Elapsed":0.3}
`

	events, err := parseTraceEvents(strings.NewReader(input), 0, nil)
	require.NoError(t, err)
	assertNoOverlappingTraceSlices(t, events)
	assertOneTrackPerPackage(t, events)
}

func TestTraceGeneration_OmitsSkippedAndEmptyPackages(t *testing.T) {
	t.Parallel()

	input := `{"Time":"2026-06-03T12:00:00.000000Z","Action":"skip","Package":"skipped_only"}
{"Time":"2026-06-03T12:00:00.010000Z","Action":"start","Package":"empty_pkg"}
{"Time":"2026-06-03T12:00:00.020000Z","Action":"pass","Package":"empty_pkg","Elapsed":0.01}
{"Time":"2026-06-03T12:00:00.030000Z","Action":"start","Package":"all_skip"}
{"Time":"2026-06-03T12:00:00.040000Z","Action":"run","Package":"all_skip","Test":"TestA"}
{"Time":"2026-06-03T12:00:00.050000Z","Action":"skip","Package":"all_skip","Test":"TestA","Elapsed":0.01}
{"Time":"2026-06-03T12:00:00.060000Z","Action":"pass","Package":"all_skip","Elapsed":0.03}
{"Time":"2026-06-03T12:00:00.070000Z","Action":"start","Package":"mixed"}
{"Time":"2026-06-03T12:00:00.080000Z","Action":"run","Package":"mixed","Test":"TestSkip"}
{"Time":"2026-06-03T12:00:00.090000Z","Action":"skip","Package":"mixed","Test":"TestSkip","Elapsed":0.01}
{"Time":"2026-06-03T12:00:00.100000Z","Action":"run","Package":"mixed","Test":"TestPass"}
{"Time":"2026-06-03T12:00:00.110000Z","Action":"pass","Package":"mixed","Test":"TestPass","Elapsed":0.01}
{"Time":"2026-06-03T12:00:00.120000Z","Action":"pass","Package":"mixed","Elapsed":0.05}
`

	events, err := parseTraceEvents(strings.NewReader(input), 0, nil)
	require.NoError(t, err)

	packages := map[string]bool{}
	for _, ev := range events {
		if ev.Ph == "M" {
			continue
		}
		if pkg := safeStringArg(ev.Args, "package"); pkg != "" {
			packages[pkg] = true
		}
	}

	assert.False(t, packages["skipped_only"])
	assert.False(t, packages["empty_pkg"])
	assert.False(t, packages["all_skip"])
	assert.True(t, packages["mixed"])
}

func TestOmitPackageFromTrace(t *testing.T) {
	t.Parallel()

	assert.True(t, omitPackageFromTrace(&pkgTraceStats{}))
	assert.True(t, omitPackageFromTrace(&pkgTraceStats{testEnds: 2, skipEnds: 2}))
	assert.False(t, omitPackageFromTrace(&pkgTraceStats{packageFail: true}))
	assert.False(t, omitPackageFromTrace(&pkgTraceStats{passOrFail: true}))
	assert.False(t, omitPackageFromTrace(&pkgTraceStats{incomplete: true}))
	assert.False(t, omitPackageFromTrace(&pkgTraceStats{testEnds: 2, skipEnds: 1, passOrFail: true}))
}

func TestTraceGeneration_OutcomeColors(t *testing.T) {
	t.Parallel()

	input := `{"Time":"2026-06-03T12:00:00.000000Z","Action":"start","Package":"pkg1"}
{"Time":"2026-06-03T12:00:00.100000Z","Action":"run","Package":"pkg1","Test":"TestPass"}
{"Time":"2026-06-03T12:00:00.200000Z","Action":"pass","Package":"pkg1","Test":"TestPass","Elapsed":0.1}
{"Time":"2026-06-03T12:00:00.300000Z","Action":"run","Package":"pkg1","Test":"TestFail"}
{"Time":"2026-06-03T12:00:00.400000Z","Action":"fail","Package":"pkg1","Test":"TestFail","Elapsed":0.1}
{"Time":"2026-06-03T12:00:00.500000Z","Action":"run","Package":"pkg1","Test":"TestSkip"}
{"Time":"2026-06-03T12:00:00.600000Z","Action":"skip","Package":"pkg1","Test":"TestSkip","Elapsed":0.1}
{"Time":"2026-06-03T12:00:00.700000Z","Action":"pass","Package":"pkg1","Elapsed":0.7}
`

	events, err := parseTraceEvents(strings.NewReader(input), 0, nil)
	require.NoError(t, err)

	cnames := map[string]string{}
	for _, ev := range events {
		if ev.Cname == "" {
			continue
		}
		cnames[ev.Name] = ev.Cname
	}

	assert.Equal(t, "good", cnames["TestPass"])
	assert.Equal(t, "bad", cnames["TestFail"])
	assert.Equal(t, "grey", cnames["TestSkip"])
	assert.Equal(t, "good", cnames["pkg1"])
}

func assertOneTrackPerPackage(t *testing.T, events []TraceEvent) {
	t.Helper()

	pkgTids := map[string]int{}
	for _, ev := range events {
		if ev.Ph == "M" {
			continue
		}
		pkg := safeStringArg(ev.Args, "package")
		if pkg == "" {
			continue
		}
		if tid, ok := pkgTids[pkg]; ok {
			assert.Equal(t, tid, ev.Tid, "package %q should use one track", pkg)
			continue
		}
		pkgTids[pkg] = ev.Tid
	}
}

func assertNoOverlappingTraceSlices(t *testing.T, events []TraceEvent) {
	t.Helper()

	type slice struct {
		start, end int64
		name       string
	}
	byTrack := make(map[[2]int][]slice)
	for _, ev := range events {
		if ev.Ph != "X" {
			continue
		}
		track := [2]int{ev.Pid, ev.Tid}
		byTrack[track] = append(byTrack[track], slice{
			start: ev.Ts,
			end:   ev.Ts + ev.Dur,
			name:  ev.Name,
		})
	}

	for track, slices := range byTrack {
		sort.Slice(slices, func(i, j int) bool { return slices[i].start < slices[j].start })
		for i := 1; i < len(slices); i++ {
			prev, cur := slices[i-1], slices[i]
			require.GreaterOrEqualf(t, cur.start, prev.end,
				"overlapping X events on pid=%d tid=%d: %q [%d,%d) overlaps %q [%d,%d)",
				track[0], track[1], prev.name, prev.start, prev.end, cur.name, cur.start, cur.end)
		}
	}
}

func TestTraceGeneration(t *testing.T) {
	t.Parallel()

	// 1. Arrange: Provide simulated go test -json output with precise Timestamps
	input := `{"Time":"2026-06-03T12:00:00.000000Z","Action":"start","Package":"pkg1"}
{"Time":"2026-06-03T12:00:00.100000Z","Action":"run","Package":"pkg1","Test":"TestA"}
{"Time":"2026-06-03T12:00:00.200000Z","Action":"pass","Package":"pkg1","Test":"TestA","Elapsed":0.1}
{"Time":"2026-06-03T12:00:00.300000Z","Action":"pass","Package":"pkg1","Elapsed":0.3}
`

	// 2. Act: Parse using a new function or helper that handles tracing
	events, err := parseTraceEvents(strings.NewReader(input), 0, nil)
	require.NoError(t, err)

	// 3. Assert: Verify the trace events generated match expectations
	assert.NotEmpty(t, events)

	var pkgEvent *TraceEvent
	var testBegin, testEnd *TraceEvent
	for i := range events {
		ev := &events[i]
		switch {
		case ev.Name == "pkg1" && ev.Ph == "X":
			pkgEvent = ev
		case ev.Name == "TestA" && ev.Ph == "b":
			testBegin = ev
		case ev.Name == "TestA" && ev.Ph == "e":
			testEnd = ev
		}
	}

	require.NotNil(t, pkgEvent, "should have pkg1 trace event")
	require.NotNil(t, testBegin, "should have TestA async begin")
	require.NotNil(t, testEnd, "should have TestA async end")

	// Timestamps are in microseconds.
	// pkg1 started at 12:00:00.000000Z, ended at 12:00:00.300000Z -> 300,000 microseconds
	// TestA started at 12:00:00.100000Z, ended at 12:00:00.200000Z -> 100,000 microseconds
	assert.Equal(t, int64(300000), pkgEvent.Dur)
	assert.Equal(t, int64(100000), testEnd.Ts-testBegin.Ts)
	assert.Equal(t, "good", pkgEvent.Cname)
	assert.Equal(t, "good", testEnd.Cname)
	assert.Equal(t, testBegin.Tid, pkgEvent.Tid, "package and tests share one track")

	assert.Greater(t, testBegin.Ts, pkgEvent.Ts, "Test should start after package")
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
				var pkgEvent *TraceEvent
				var testBegin, testEnd *TraceEvent
				for i := range events {
					ev := &events[i]
					switch {
					case ev.Name == "pkg1" && ev.Ph == "X":
						pkgEvent = ev
					case ev.Name == "TestA" && ev.Ph == "b":
						testBegin = ev
					case ev.Name == "TestA" && ev.Ph == "e":
						testEnd = ev
					}
				}
				require.NotNil(t, pkgEvent)
				require.NotNil(t, testBegin)
				require.NotNil(t, testEnd)
				// They both ended at last seen time (12:00:00.100000Z)
				assert.Equal(t, int64(100000), pkgEvent.Dur)
				assert.Equal(t, int64(0), testEnd.Ts-testBegin.Ts)
				assert.Equal(t, "rail_response", pkgEvent.Cname)
				assert.Equal(t, "rail_response", testEnd.Cname)
			},
		},
		{
			name:  "missing start event (using elapsed fallback)",
			input: `{"Time":"2026-06-03T12:00:00.200000Z","Action":"pass","Package":"pkg1","Test":"TestA","Elapsed":0.15}`,
			expectedEvents: func(t *testing.T, events []TraceEvent) {
				var testBegin, testEnd *TraceEvent
				for i := range events {
					ev := &events[i]
					switch {
					case ev.Name == "TestA" && ev.Ph == "b":
						testBegin = ev
					case ev.Name == "TestA" && ev.Ph == "e":
						testEnd = ev
					}
				}
				require.NotNil(t, testBegin)
				require.NotNil(t, testEnd)
				assert.Equal(t, int64(150000), testEnd.Ts-testBegin.Ts)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			events, err := parseTraceEvents(strings.NewReader(tc.input), 0, nil)
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
{"Time":"2026-06-03T12:00:00.100000Z","Action":"run","Package":"pkg1","Test":"TestA"}
{"Time":"2026-06-03T12:00:00.200000Z","Action":"pass","Package":"pkg1","Test":"TestA","Elapsed":0.1}
{"Time":"2026-06-03T12:00:00.300000Z","Action":"pass","Package":"pkg1","Elapsed":0.3}
`
	log2 := `{"Time":"2026-06-03T12:01:00.000000Z","Action":"start","Package":"pkg1"}
{"Time":"2026-06-03T12:01:00.050000Z","Action":"run","Package":"pkg1","Test":"TestA"}
{"Time":"2026-06-03T12:01:00.150000Z","Action":"pass","Package":"pkg1","Test":"TestA","Elapsed":0.1}
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

	err = WriteTrace(nil, tmpDir, report)
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

	// Verify specific process name metadata and duration events
	var hasProc1, hasProc2, hasThreadName, hasPkg1Event, hasPkg2Event bool
	for _, ev := range traceEvents {
		if ev.Name == "process_name" && ev.Pid == 1 && ev.Args["name"] == "Iteration 1 (Seed 123)" {
			hasProc1 = true
		}
		if ev.Name == "process_name" && ev.Pid == 2 && ev.Args["name"] == "Iteration 2 (Seed 456)" {
			hasProc2 = true
		}
		if ev.Name == "thread_name" && ev.Pid == 1 && ev.Args["name"] == "pkg1" {
			hasThreadName = true
		}
		if ev.Name == "pkg1" && ev.Ph == "X" {
			if ev.Pid == 1 && ev.Dur == 300000 {
				hasPkg1Event = true
			}
			if ev.Pid == 2 && ev.Dur == 200000 {
				hasPkg2Event = true
			}
		}
	}
	assert.True(t, hasProc1, "should have process name for Iteration 1")
	assert.True(t, hasProc2, "should have process name for Iteration 2")
	assert.True(t, hasThreadName, "should have thread name for pkg1")
	assert.True(t, hasPkg1Event, "should have pkg1 event in Iteration 1 (300ms)")
	assert.True(t, hasPkg2Event, "should have pkg1 event in Iteration 2 (200ms)")
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
		done <- ServeTrace(ctx, tmpDir, nil, TraceServeOptions{
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

	events, err := parseTraceEvents(strings.NewReader(input), 0, nil)
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

	err := WriteTrace(out, tmpDir, nil)
	require.NoError(t, err)

	content, err := os.ReadFile(filepath.Join(tmpDir, "trace.json")) //nolint:gosec // G304: path from filepath.Join
	require.NoError(t, err)
	assert.Equal(t, "[]", strings.TrimSpace(string(content)))
	assert.Contains(t, stderr.String(), "warning: no test execution events recorded in trace")
}

func TestWriteTrace_InvalidIterationName(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	err := os.WriteFile(filepath.Join(tmpDir, "iteration-invalid.log.jsonl"), []byte("{}"), 0600)
	require.NoError(t, err)

	err = WriteTrace(nil, tmpDir, &Report{})
	require.NoError(t, err)
}

func TestParseTraceEvents_MalformedJSONL(t *testing.T) {
	t.Parallel()

	var stderr strings.Builder
	out := output.NewForTest(false, io.Discard, &stderr, false)
	input := "{not valid json}\n" +
		`{"Time":"2026-06-03T12:00:00.000000Z","Action":"run","Package":"pkg1","Test":"TestA"}` + "\n" +
		`{"Time":"2026-06-03T12:00:00.100000Z","Action":"pass","Package":"pkg1","Test":"TestA","Elapsed":0.1}` + "\n"

	events, err := parseTraceEvents(strings.NewReader(input), 2, out)
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

func TestTraceActionCname(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "good", traceActionCname("pass", false))
	assert.Equal(t, "bad", traceActionCname("fail", false))
	assert.Equal(t, "grey", traceActionCname("skip", false))
	assert.Equal(t, "rail_response", traceActionCname("pass", true))
}

func TestSafeStringArg(t *testing.T) {
	t.Parallel()
	assert.Empty(t, safeStringArg(nil, "package"))
	assert.Empty(t, safeStringArg(map[string]any{}, "package"))
	assert.Empty(t, safeStringArg(map[string]any{"package": 123}, "package"))
	assert.Equal(t, "pkg", safeStringArg(map[string]any{"package": "pkg"}, "package"))
}
