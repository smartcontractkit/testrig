package runner

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/term"

	"github.com/smartcontractkit/testrig/internal/output"
)

// traceListenAddr is the fixed local address Perfetto loads trace.json from.
// Production always uses this port; tests bind an ephemeral port via TraceServeOptions.Addr.
const traceListenAddr = "127.0.0.1:9001"

// TraceEvent represents a single event in Chrome's Trace Event Format.
type TraceEvent struct {
	Name  string         `json:"name"`
	Cat   string         `json:"cat"`
	Ph    string         `json:"ph"`  // "X" for Complete event, "M" for Metadata
	Ts    int64          `json:"ts"`  // Microseconds
	Dur   int64          `json:"dur"` // Microseconds
	Pid   int            `json:"pid"`
	Tid   int            `json:"tid"`
	Cname string         `json:"cname,omitempty"`
	Args  map[string]any `json:"args,omitempty"`
}

type traceKey struct {
	Package string
	Test    string
}

// traceViewerEnabled reports whether diagnose should serve trace.json and open a browser.
func traceViewerEnabled() bool {
	if v := os.Getenv("TESTRIG_NO_BROWSER"); v != "" && v != "0" && !strings.EqualFold(v, "false") {
		return false
	}
	if os.Getenv("CI") != "" {
		return false
	}
	return term.IsTerminal(int(os.Stderr.Fd()))
}

const maxOutputSize = 8 * 1024 // 8 KB

type testState struct {
	pkg        string
	name       string
	runTime    time.Time
	pauseTime  time.Time
	contTime   time.Time
	endTime    time.Time
	status     string
	output     []byte
	truncated  bool
	isParallel bool
	incomplete bool
	elapsed    float64
}

func (ts *testState) addOutput(s string) {
	ts.output = append(ts.output, s...)
	if len(ts.output) > maxOutputSize {
		ts.output = ts.output[len(ts.output)-maxOutputSize:]
		ts.truncated = true
	}
}

func (ts *testState) getOutput() string {
	if ts.truncated {
		return "\n... (truncated)\n" + string(ts.output)
	}
	return string(ts.output)
}

// parseTraceEvents parses a go test -json stream for a single iteration and returns trace events.
func parseTraceEvents(
	r io.Reader,
	iter int,
	out *output.Printer,
	pkgIndexes map[string]int,
) ([]TraceEvent, error) {
	states, lastSeenTime, uniquePackages, err := scanTraceEvents(r, iter, out)
	if err != nil {
		return nil, err
	}

	allTests, pkgHasRunTests := prepareTestStates(states, lastSeenTime)
	orderPackages(allTests, uniquePackages, pkgHasRunTests, pkgIndexes)

	events, pkgThreads := generateTraceEvents(allTests, pkgIndexes, iter)
	events = appendMetadataEvents(events, uniquePackages, pkgIndexes, pkgThreads)

	return events, nil
}

type traceTestEvent struct {
	Time        time.Time `json:"Time"`
	Action      string    `json:"Action"`
	Package     string    `json:"Package"`
	Test        string    `json:"Test"`
	Elapsed     float64   `json:"Elapsed"`
	Output      string    `json:"Output"`
	FailedBuild string    `json:"FailedBuild,omitempty"`
}

func scanTraceEvents(
	r io.Reader,
	iter int,
	out *output.Printer,
) (map[traceKey]*testState, time.Time, map[string]bool, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 1024*1024), 10*1024*1024)

	states := make(map[traceKey]*testState)
	getState := func(key traceKey) *testState {
		s, ok := states[key]
		if !ok {
			s = &testState{pkg: key.Package, name: key.Test}
			states[key] = s
		}
		return s
	}

	var lastSeenTime time.Time
	uniquePackages := make(map[string]bool)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 || line[0] != '{' {
			continue
		}
		var ev traceTestEvent
		if err := json.Unmarshal(line, &ev); err != nil {
			if out != nil {
				out.Stderrf("trace: skip malformed jsonl (iteration %d): %v\n", iter, err)
			}
			continue
		}
		if ev.Time.IsZero() {
			continue
		}
		lastSeenTime = ev.Time
		if ev.Package != "" {
			uniquePackages[ev.Package] = true
		}

		key := traceKey{Package: ev.Package, Test: ev.Test}
		ts := getState(key)

		switch ev.Action {
		case "start":
			if ts.name == "" && ts.runTime.IsZero() {
				ts.runTime = ev.Time
			}
		case "run":
			if ts.name != "" && ts.runTime.IsZero() {
				ts.runTime = ev.Time
			}
		case "pause":
			ts.pauseTime = ev.Time
			ts.isParallel = true
		case "cont":
			ts.contTime = ev.Time
		case "output":
			ts.addOutput(ev.Output)
		case "pass", "fail", "skip":
			ts.endTime = ev.Time
			ts.status = ev.Action
			ts.elapsed = ev.Elapsed
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, time.Time{}, nil, err
	}
	return states, lastSeenTime, uniquePackages, nil
}

func prepareTestStates(states map[traceKey]*testState, lastSeenTime time.Time) ([]*testState, map[string]bool) {
	// Handle incomplete
	for _, ts := range states {
		if ts.endTime.IsZero() && !ts.runTime.IsZero() && !lastSeenTime.IsZero() {
			ts.endTime = lastSeenTime
			ts.incomplete = true
		}
	}

	pkgHasRunTests := make(map[string]bool)
	for _, ts := range states {
		if ts.name != "" && ts.status != "skip" {
			pkgHasRunTests[ts.pkg] = true
		}
	}

	var allTests []*testState
	for _, ts := range states {
		if !ts.runTime.IsZero() && !ts.endTime.IsZero() {
			allTests = append(allTests, ts)
		} else if ts.status != "" {
			// missing start event fallback
			ts.runTime = ts.endTime.Add(-time.Duration(ts.elapsed * float64(time.Second)))
			allTests = append(allTests, ts)
		}
	}
	return allTests, pkgHasRunTests
}

func orderPackages(allTests []*testState, uniquePackages, pkgHasRunTests map[string]bool, pkgIndexes map[string]int) {
	pkgEarliestRun := make(map[string]time.Time)
	for _, ts := range allTests {
		current, ok := pkgEarliestRun[ts.pkg]
		if !ok || ts.runTime.Before(current) {
			pkgEarliestRun[ts.pkg] = ts.runTime
		}
	}

	var orderedPkgs []string
	for pkg := range uniquePackages {
		if pkgHasRunTests[pkg] {
			orderedPkgs = append(orderedPkgs, pkg)
		}
	}
	sort.Slice(orderedPkgs, func(i, j int) bool {
		return pkgEarliestRun[orderedPkgs[i]].Before(pkgEarliestRun[orderedPkgs[j]])
	})

	for _, pkg := range orderedPkgs {
		if _, ok := pkgIndexes[pkg]; !ok {
			pkgIndexes[pkg] = len(pkgIndexes) + 1
		}
	}
}

type threadAlloc struct {
	endTime time.Time
}

func generateTraceEvents(
	allTests []*testState,
	pkgIndexes map[string]int,
	iter int,
) ([]TraceEvent, map[string][]threadAlloc) {
	sort.Slice(allTests, func(i, j int) bool {
		t1 := allTests[i].contTime
		if t1.IsZero() {
			t1 = allTests[i].runTime
		}
		t2 := allTests[j].contTime
		if t2.IsZero() {
			t2 = allTests[j].runTime
		}
		return t1.Before(t2)
	})

	var events []TraceEvent
	pkgThreads := make(map[string][]threadAlloc)

	allocSlot := func(pkg string, start, end time.Time) int {
		threads := pkgThreads[pkg]
		tid := -1
		for i, th := range threads {
			if !start.Before(th.endTime) {
				threads[i].endTime = end
				tid = i
				break
			}
		}
		if tid == -1 {
			threads = append(threads, threadAlloc{endTime: end})
			pkgThreads[pkg] = threads
			tid = len(threads) - 1
		}
		return tid
	}

	for _, ts := range allTests {
		pid, ok := pkgIndexes[ts.pkg]
		if !ok {
			continue
		}

		cname := getTraceCname(ts)
		cat := "test"
		name := ts.name
		if name == "" {
			name = ts.pkg
			cat = "package"
		}

		args := createTraceEventArgs(ts, iter)

		if ts.isParallel && !ts.pauseTime.IsZero() && !ts.contTime.IsZero() {
			dur1 := max(ts.pauseTime.Sub(ts.runTime).Microseconds(), 0)
			if dur1 > 0 {
				tid := allocSlot(ts.pkg, ts.runTime, ts.pauseTime)
				events = append(events, TraceEvent{
					Name:  name,
					Cat:   cat,
					Ph:    "X",
					Ts:    ts.runTime.UnixMicro(),
					Dur:   dur1,
					Pid:   pid,
					Tid:   1 + tid,
					Cname: cname,
					Args:  args,
				})
			}

			durExec := max(ts.endTime.Sub(ts.contTime).Microseconds(), 0)
			if durExec == 0 && ts.elapsed > 0 {
				durExec = int64(ts.elapsed * 1e6)
			}
			tid := allocSlot(ts.pkg, ts.contTime, ts.endTime)
			events = append(events, TraceEvent{
				Name:  name,
				Cat:   cat,
				Ph:    "X",
				Ts:    ts.contTime.UnixMicro(),
				Dur:   durExec,
				Pid:   pid,
				Tid:   1 + tid,
				Cname: cname,
				Args:  args,
			})
		} else {
			dur := max(ts.endTime.Sub(ts.runTime).Microseconds(), 0)
			if dur == 0 && ts.elapsed > 0 {
				dur = int64(ts.elapsed * 1e6)
			}
			tid := 0
			if cat != "package" {
				tid = 1 + allocSlot(ts.pkg, ts.runTime, ts.endTime)
			}
			events = append(events, TraceEvent{
				Name:  name,
				Cat:   cat,
				Ph:    "X",
				Ts:    ts.runTime.UnixMicro(),
				Dur:   dur,
				Pid:   pid,
				Tid:   tid,
				Cname: cname,
				Args:  args,
			})
		}
	}
	return events, pkgThreads
}

func getTraceCname(ts *testState) string {
	if ts.incomplete {
		return "terrible"
	}
	switch ts.status {
	case "pass":
		return "good"
	case "fail":
		return "bad"
	case "skip":
		return "yellow"
	default:
		return "terrible"
	}
}

func createTraceEventArgs(ts *testState, iter int) map[string]any {
	args := map[string]any{
		"package":   ts.pkg,
		"status":    ts.status,
		"iteration": iter + 1,
	}
	if ts.name != "" {
		args["test_id"] = fmt.Sprintf("%s.%s", ts.pkg, ts.name)
	} else {
		args["test_id"] = ts.pkg
	}
	if ts.isParallel {
		args["is_parallel"] = true
	}
	if ts.incomplete {
		args["incomplete"] = true
	}
	outStr := ts.getOutput()
	if outStr != "" {
		args["output"] = outStr
	}
	return args
}

func appendMetadataEvents(
	events []TraceEvent,
	uniquePackages map[string]bool,
	pkgIndexes map[string]int,
	pkgThreads map[string][]threadAlloc,
) []TraceEvent {
	for pkg := range uniquePackages {
		pid, ok := pkgIndexes[pkg]
		if !ok {
			continue
		}

		events = append(events, TraceEvent{
			Name: "process_name",
			Ph:   "M",
			Pid:  pid,
			Args: map[string]any{"name": pkg},
		})
		events = append(events, TraceEvent{
			Name: "process_sort_index",
			Ph:   "M",
			Pid:  pid,
			Args: map[string]any{"sort_index": pid},
		})

		events = append(events, TraceEvent{
			Name: "thread_name",
			Ph:   "M",
			Pid:  pid,
			Tid:  0,
			Args: map[string]any{"name": "Main"},
		})
		events = append(events, TraceEvent{
			Name: "thread_sort_index",
			Ph:   "M",
			Pid:  pid,
			Tid:  0,
			Args: map[string]any{"sort_index": 0},
		})

		for i := range pkgThreads[pkg] {
			events = append(events, TraceEvent{
				Name: "thread_name",
				Ph:   "M",
				Pid:  pid,
				Tid:  1 + i,
				Args: map[string]any{"name": fmt.Sprintf("Thread %d", i+1)},
			})
			events = append(events, TraceEvent{
				Name: "thread_sort_index",
				Ph:   "M",
				Pid:  pid,
				Tid:  1 + i,
				Args: map[string]any{"sort_index": 1 + i},
			})
		}
	}
	return events
}

// WriteTrace parses iteration logs under resultsDir and writes a separate trace-<N>.json file per iteration.
// It returns a list of the generated filenames.
func WriteTrace(out *output.Printer, resultsDir string) ([]string, error) {
	matches, err := filepath.Glob(filepath.Join(resultsDir, "iteration-*.log.jsonl"))
	if err != nil {
		return nil, err
	}
	sort.Slice(matches, func(i, j int) bool {
		return iterNumber(matches[i]) < iterNumber(matches[j])
	})

	var generatedFiles []string
	pkgIndexes := make(map[string]int)

	for _, path := range matches {
		iter := iterNumber(path)
		if iter < 0 {
			continue
		}
		f, err := os.Open(path) //nolint:gosec // G304: path from filepath.Glob
		if err != nil {
			return nil, err
		}
		events, err := parseTraceEvents(f, iter, out, pkgIndexes)
		_ = f.Close()
		if err != nil {
			return nil, err
		}

		if len(events) > 0 {
			b, err := json.MarshalIndent(events, "", "  ")
			if err != nil {
				return nil, err
			}
			filename := fmt.Sprintf("trace-%d.json", iter+1)
			if err := os.WriteFile(filepath.Join(resultsDir, filename), b, 0600); err != nil {
				return nil, err
			}
			generatedFiles = append(generatedFiles, filename)
		}
	}

	if len(generatedFiles) == 0 && out != nil {
		out.Stderrf("warning: no test execution events recorded in trace\n")
	}

	return generatedFiles, nil
}

// TraceServeOptions groups parameters for ServeTrace.
type TraceServeOptions struct {
	// Addr overrides traceListenAddr. Production leaves this empty; tests set "127.0.0.1:0".
	Addr        string
	OpenBrowser func(string) error
}

// ServeTrace serves trace files at traceListenAddr, opens the Perfetto UI in browser tabs,
// and blocks until all traces are fetched or ctx is cancelled.
func ServeTrace(
	ctx context.Context,
	resultsDir string,
	files []string,
	out *output.Printer,
	opts TraceServeOptions,
) error {
	if len(files) == 0 {
		return fmt.Errorf("no trace files provided to ServeTrace")
	}
	for _, f := range files {
		tracePath := filepath.Join(resultsDir, f)
		if _, err := os.Stat(tracePath); err != nil {
			return fmt.Errorf("trace file not found at %s: %w", tracePath, err)
		}
	}

	addr := opts.Addr
	if addr == "" {
		addr = traceListenAddr
	}
	openBrowserCB := opts.OpenBrowser
	if openBrowserCB == nil {
		openBrowserCB = openBrowser
	}

	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf(
			"failed to bind local port %s (is another trace server or perfetto instance running?): %w",
			addr,
			err,
		)
	}
	defer func() { _ = listener.Close() }()

	srvCtx, srvCancel := context.WithCancel(ctx)
	defer srvCancel()

	var fetches int32
	var fetchesMu sync.Mutex

	mux := http.NewServeMux()
	for _, file := range files {
		mux.HandleFunc("/"+file, func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin != "" && origin != "https://ui.perfetto.dev" {
				http.Error(w, "Forbidden origin", http.StatusForbidden)
				return
			}
			w.Header().Set("Access-Control-Allow-Origin", "https://ui.perfetto.dev")
			w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
			w.Header().Set("Access-Control-Allow-Private-Network", "true")
			if r.Method == "OPTIONS" {
				return
			}

			// Serve the requested file
			reqFile := filepath.Base(r.URL.Path)
			tracePath := filepath.Join(resultsDir, reqFile)
			http.ServeFile(w, r, tracePath)

			// Safely count fetches. Wait until all files have been fetched.
			fetchesMu.Lock()
			fetches++
			allFetched := int(fetches) >= len(files)
			fetchesMu.Unlock()

			if allFetched {
				// Auto-exit after all files are served
				go func() {
					time.Sleep(100 * time.Millisecond)
					srvCancel()
				}()
			}
		})
	}

	server := &http.Server{
		Handler: mux,
	}

	go func() {
		_ = server.Serve(listener)
	}()

	if out != nil {
		out.HumanStderr("\nOpening perfetto.dev trace tabs in browser...\n")
	} else {
		fmt.Printf("\nOpening perfetto.dev trace tabs in browser...\n")
	}

	for _, file := range files {
		perfettoURL := fmt.Sprintf("https://ui.perfetto.dev/#!/?url=http://%s/%s", listener.Addr().String(), file)
		if err := openBrowserCB(perfettoURL); err != nil {
			msg := fmt.Sprintf("open browser: %v\nOpen manually: %s\n", err, perfettoURL)
			if out != nil {
				out.Stderrf("%s", msg)
			} else {
				fmt.Fprint(os.Stderr, msg)
			}
		}
		time.Sleep(100 * time.Millisecond) // Slight pause to prevent browser throttling
	}

	<-srvCtx.Done()

	// Graceful shutdown
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	_ = server.Shutdown(shutdownCtx)

	return nil
}

func openBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		//nolint:gosec // G204: user-controlled url for opening browser
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		//nolint:gosec // G204: user-controlled url for opening browser
		cmd = exec.Command("open", url)
	default:
		//nolint:gosec // G204: user-controlled url for opening browser
		cmd = exec.Command("xdg-open", url)
	}
	return cmd.Run()
}

func safeStringArg(args map[string]any, key string) string {
	if args == nil {
		return ""
	}
	val, ok := args[key]
	if !ok {
		return ""
	}
	s, ok := val.(string)
	if !ok {
		return ""
	}
	return s
}
