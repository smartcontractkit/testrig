package runner

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
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
	Ph    string         `json:"ph"`            // X complete, b/e nestable async, M metadata
	Ts    int64          `json:"ts"`            // Microseconds
	Dur   int64          `json:"dur,omitempty"` // Microseconds (X only)
	Pid   int            `json:"pid"`
	Tid   int            `json:"tid"`
	ID    string         `json:"id,omitempty"`    // Nestable async slice id (tests)
	Cname string         `json:"cname,omitempty"` // Perfetto slice color
	Args  map[string]any `json:"args,omitempty"`
}

type traceKey struct {
	Package string
	Test    string
}

// pkgTraceStats summarizes per-package outcomes to decide trace inclusion.
type pkgTraceStats struct {
	packageFail bool
	passOrFail  bool // at least one test passed or failed
	incomplete  bool
	testEnds    int
	skipEnds    int
}

func statsFor(m map[string]*pkgTraceStats, pkg string) *pkgTraceStats {
	if s, ok := m[pkg]; ok {
		return s
	}
	s := &pkgTraceStats{}
	m[pkg] = s
	return s
}

func (s *pkgTraceStats) recordTestEnd(action string, incomplete bool) {
	s.testEnds++
	if incomplete {
		s.incomplete = true
		return
	}
	switch action {
	case "pass", "fail":
		s.passOrFail = true
	case "skip":
		s.skipEnds++
	}
}

// omitPackageFromTrace reports whether a package should be dropped from the trace.
// Packages with no tests run, or where every test was skipped, are omitted unless
// they failed at package scope or had an incomplete (e.g. timeout) test.
func omitPackageFromTrace(s *pkgTraceStats) bool {
	if s.packageFail || s.passOrFail || s.incomplete {
		return false
	}
	if s.testEnds == 0 {
		return true
	}
	return s.testEnds == s.skipEnds
}

func omittedPackages(pkgStats map[string]*pkgTraceStats) map[string]bool {
	omitted := make(map[string]bool, len(pkgStats))
	for pkg, st := range pkgStats {
		if omitPackageFromTrace(st) {
			omitted[pkg] = true
		}
	}
	return omitted
}

func filterTraceEventsByPackage(events []TraceEvent, omitted map[string]bool) []TraceEvent {
	if len(omitted) == 0 {
		return events
	}
	filtered := events[:0]
	for _, ev := range events {
		pkg := safeStringArg(ev.Args, "package")
		if pkg != "" && omitted[pkg] {
			continue
		}
		filtered = append(filtered, ev)
	}
	return filtered
}

func includedPackages(pkgStats map[string]*pkgTraceStats, omitted map[string]bool) []string {
	var pkgs []string
	for pkg := range pkgStats {
		if omitted[pkg] {
			continue
		}
		pkgs = append(pkgs, pkg)
	}
	sort.Strings(pkgs)
	return pkgs
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

// traceActionCname maps go test outcomes to Chrome trace color names for Perfetto.
func traceActionCname(action string, incomplete bool) string {
	if incomplete {
		return "rail_response"
	}
	switch action {
	case "pass":
		return "good"
	case "fail":
		return "bad"
	case "skip":
		return "grey"
	default:
		return ""
	}
}

// parseTraceEvents parses a go test -json stream for a single iteration and returns trace events.
// Each package is one Perfetto track. Package duration uses X events; tests use nestable async
// b/e slices on the same track so parallel tests do not produce overlapping X events.
func parseTraceEvents(r io.Reader, iter int, out *output.Printer) ([]TraceEvent, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 1024*1024), 10*1024*1024)
	startTimes := make(map[traceKey]time.Time)
	asyncIDs := make(map[traceKey]string)
	var events []TraceEvent
	var lastSeenTime time.Time
	pkgStats := make(map[string]*pkgTraceStats)

	pid := iter + 1
	var nextAsyncID uint64

	allocAsyncID := func() string {
		nextAsyncID++
		return fmt.Sprintf("0x%x", nextAsyncID)
	}

	appendTestBegin := func(key traceKey, name, pkg string, ts time.Time) string {
		id := allocAsyncID()
		asyncIDs[key] = id
		events = append(events, TraceEvent{
			Name: name,
			Cat:  "test",
			Ph:   "b",
			Ts:   ts.UnixMicro(),
			Pid:  pid,
			ID:   id,
			Args: map[string]any{
				"package": pkg,
			},
		})
		return id
	}

	appendTestEnd := func(
		key traceKey,
		name, pkg, action string,
		start, end time.Time,
		incomplete bool,
		extraArgs map[string]any,
	) {
		id, ok := asyncIDs[key]
		if !ok {
			id = appendTestBegin(key, name, pkg, start)
		}
		delete(asyncIDs, key)
		delete(startTimes, key)

		args := map[string]any{"package": pkg}
		if action != "" {
			args["action"] = action
		}
		if incomplete {
			args["incomplete"] = true
		}
		maps.Copy(args, extraArgs)

		statsFor(pkgStats, pkg).recordTestEnd(action, incomplete)

		events = append(events, TraceEvent{
			Name:  name,
			Cat:   "test",
			Ph:    "e",
			Ts:    end.UnixMicro(),
			Pid:   pid,
			ID:    id,
			Cname: traceActionCname(action, incomplete),
			Args:  args,
		})
	}

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 || line[0] != '{' {
			continue
		}
		var ev TestEvent
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

		key := traceKey{Package: ev.Package, Test: ev.Test}

		switch ev.Action {
		case "start":
			if ev.Test == "" {
				startTimes[key] = ev.Time
			}
		case "run":
			if ev.Test != "" {
				startTimes[key] = ev.Time
				appendTestBegin(key, ev.Test, ev.Package, ev.Time)
			}
		case "pass", "fail", "skip":
			start, ok := startTimes[key]
			if !ok {
				start = ev.Time.Add(-time.Duration(ev.Elapsed * float64(time.Second)))
			}

			if ev.Test != "" {
				extra := map[string]any{"elapsed": ev.Elapsed}
				appendTestEnd(key, ev.Test, ev.Package, ev.Action, start, ev.Time, false, extra)
				continue
			}

			delete(startTimes, key)

			st := statsFor(pkgStats, ev.Package)
			if ev.Action == "fail" {
				st.packageFail = true
			}

			dur := max(ev.Time.Sub(start).Microseconds(), 0)
			if dur == 0 && ev.Elapsed > 0 {
				dur = int64(ev.Elapsed * 1e6)
			}

			events = append(events, TraceEvent{
				Name:  ev.Package,
				Cat:   "package",
				Ph:    "X",
				Ts:    start.UnixMicro(),
				Dur:   dur,
				Pid:   pid,
				Cname: traceActionCname(ev.Action, false),
				Args: map[string]any{
					"package": ev.Package,
					"action":  ev.Action,
					"elapsed": ev.Elapsed,
				},
			})
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	if !lastSeenTime.IsZero() {
		for key, start := range startTimes {
			if key.Test != "" {
				appendTestEnd(key, key.Test, key.Package, "", start, lastSeenTime, true, nil)
				continue
			}

			st := statsFor(pkgStats, key.Package)
			st.incomplete = true

			dur := max(lastSeenTime.Sub(start).Microseconds(), 0)
			events = append(events, TraceEvent{
				Name:  key.Package,
				Cat:   "package",
				Ph:    "X",
				Ts:    start.UnixMicro(),
				Dur:   dur,
				Pid:   pid,
				Cname: traceActionCname("", true),
				Args: map[string]any{
					"package":    key.Package,
					"incomplete": true,
				},
			})
		}
	}

	omitted := omittedPackages(pkgStats)
	events = filterTraceEventsByPackage(events, omitted)
	pkgs := includedPackages(pkgStats, omitted)

	pkgTids := make(map[string]int, len(pkgs))
	for i, pkg := range pkgs {
		pkgTids[pkg] = i + 1
	}

	for i := range events {
		if events[i].Ph == "M" {
			continue
		}
		pkg := safeStringArg(events[i].Args, "package")
		events[i].Tid = pkgTids[pkg]
	}

	for pkg, tid := range pkgTids {
		events = append(events, TraceEvent{
			Name: "thread_name",
			Ph:   "M",
			Pid:  pid,
			Tid:  tid,
			Args: map[string]any{
				"name": pkg,
			},
		})
		events = append(events, TraceEvent{
			Name: "thread_sort_index",
			Ph:   "M",
			Pid:  pid,
			Tid:  tid,
			Args: map[string]any{
				"sort_index": tid,
			},
		})
	}

	return events, nil
}

// WriteTrace aggregates the iteration-*.log.jsonl files under resultsDir and writes trace.json.
func WriteTrace(out *output.Printer, resultsDir string, rep *Report) error {
	matches, err := filepath.Glob(filepath.Join(resultsDir, "iteration-*.log.jsonl"))
	if err != nil {
		return err
	}
	sort.Slice(matches, func(i, j int) bool {
		return iterNumber(matches[i]) < iterNumber(matches[j])
	})

	allEvents := []TraceEvent{}

	for _, path := range matches {
		iter := iterNumber(path)
		if iter < 0 {
			continue
		}
		f, err := os.Open(path) //nolint:gosec // G304: path from filepath.Glob
		if err != nil {
			return err
		}
		events, err := parseTraceEvents(f, iter, out)
		_ = f.Close()
		if err != nil {
			return err
		}

		pid := iter + 1
		procName := fmt.Sprintf("Iteration %d", iter+1)
		if rep != nil && iter < len(rep.IterationSummaries) {
			s := rep.IterationSummaries[iter]
			if s.ShuffleSeed > 0 {
				procName = fmt.Sprintf("Iteration %d (Seed %d)", iter+1, s.ShuffleSeed)
			}
		}

		allEvents = append(allEvents, TraceEvent{
			Name: "process_name",
			Ph:   "M",
			Pid:  pid,
			Args: map[string]any{
				"name": procName,
			},
		})
		allEvents = append(allEvents, TraceEvent{
			Name: "process_sort_index",
			Ph:   "M",
			Pid:  pid,
			Args: map[string]any{
				"sort_index": pid,
			},
		})

		allEvents = append(allEvents, events...)
	}

	hasDurationEvents := false
	for _, ev := range allEvents {
		if ev.Ph == "X" || ev.Ph == "e" {
			hasDurationEvents = true
			break
		}
	}
	if !hasDurationEvents && out != nil {
		out.Stderrf("warning: no test execution events recorded in trace\n")
	}

	b, err := json.MarshalIndent(allEvents, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(resultsDir, "trace.json"), b, 0600)
}

// TraceServeOptions groups parameters for ServeTrace.
type TraceServeOptions struct {
	// Addr overrides traceListenAddr. Production leaves this empty; tests set "127.0.0.1:0".
	Addr        string
	OpenBrowser func(string) error
}

// ServeTrace serves trace.json at traceListenAddr, opens the Perfetto UI in a browser,
// and blocks until the trace is fetched or ctx is cancelled.
func ServeTrace(
	ctx context.Context,
	resultsDir string,
	out *output.Printer,
	opts TraceServeOptions,
) error {
	tracePath := filepath.Join(resultsDir, "trace.json")
	if _, err := os.Stat(tracePath); err != nil {
		return fmt.Errorf("trace file not found at %s: %w", tracePath, err)
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

	mux := http.NewServeMux()
	mux.HandleFunc("/trace.json", func(w http.ResponseWriter, r *http.Request) {
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
		http.ServeFile(w, r, tracePath)

		go func() {
			time.Sleep(100 * time.Millisecond)
			srvCancel()
		}()
	})

	server := &http.Server{
		Handler: mux,
	}

	go func() {
		_ = server.Serve(listener)
	}()

	perfettoURL := fmt.Sprintf("https://ui.perfetto.dev/#!/?url=http://%s/trace.json", listener.Addr().String())

	if out != nil {
		out.HumanStderr("\nOpening perfetto.dev trace in browser...\n")
	} else {
		fmt.Printf("\nOpening perfetto.dev trace in browser...\n")
	}

	if err := openBrowserCB(perfettoURL); err != nil {
		msg := fmt.Sprintf("open browser: %v\nOpen manually: %s\n", err, perfettoURL)
		if out != nil {
			out.Stderrf("%s", msg)
		} else {
			fmt.Fprint(os.Stderr, msg)
		}
	}

	<-srvCtx.Done()

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
