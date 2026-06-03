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
	"time"

	"github.com/smartcontractkit/testrig/internal/output"
)

// TraceEvent represents a single event in Chrome's Trace Event Format.
type TraceEvent struct {
	Name string         `json:"name"`
	Cat  string         `json:"cat"`
	Ph   string         `json:"ph"`  // "X" for Complete event, "M" for Metadata
	Ts   int64          `json:"ts"`  // Microseconds
	Dur  int64          `json:"dur"` // Microseconds
	Pid  int            `json:"pid"`
	Tid  int            `json:"tid"`
	Args map[string]any `json:"args,omitempty"`
}

type traceKey struct {
	Package string
	Test    string
}

// parseTraceEvents parses a go test -json stream for a single iteration and returns trace events.
func parseTraceEvents(r io.Reader, iter int) ([]TraceEvent, error) {
	scanner := bufio.NewScanner(r)
	startTimes := make(map[traceKey]time.Time)
	var events []TraceEvent
	var lastSeenTime time.Time
	uniquePackages := make(map[string]bool)

	pid := iter + 1

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 || line[0] != '{' {
			continue
		}
		var ev TestEvent
		if err := json.Unmarshal(line, &ev); err != nil {
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

		switch ev.Action {
		case "start":
			if ev.Test == "" {
				startTimes[key] = ev.Time
			}
		case "run":
			if ev.Test != "" {
				startTimes[key] = ev.Time
			}
		case "pass", "fail", "skip":
			start, ok := startTimes[key]
			if !ok {
				// Fallback to estimating start time using Elapsed.
				start = ev.Time.Add(-time.Duration(ev.Elapsed * float64(time.Second)))
			} else {
				delete(startTimes, key)
			}

			dur := max(ev.Time.Sub(start).Microseconds(), 0)
			// If dur is calculated as 0 but Elapsed is > 0, fallback to Elapsed.
			if dur == 0 && ev.Elapsed > 0 {
				dur = int64(ev.Elapsed * 1e6)
			}

			name := ev.Test
			cat := "test"
			if ev.Test == "" {
				name = ev.Package
				cat = "package"
			}

			events = append(events, TraceEvent{
				Name: name,
				Cat:  cat,
				Ph:   "X",
				Ts:   start.UnixMicro(),
				Dur:  dur,
				Pid:  pid,
				// Tid will be populated later after packages are sorted.
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

	// Handle incomplete/timed out tests/packages
	if !lastSeenTime.IsZero() {
		for key, start := range startTimes {
			dur := max(lastSeenTime.Sub(start).Microseconds(), 0)
			name := key.Test
			cat := "test"
			if key.Test == "" {
				name = key.Package
				cat = "package"
			}
			events = append(events, TraceEvent{
				Name: name,
				Cat:  cat,
				Ph:   "X",
				Ts:   start.UnixMicro(),
				Dur:  dur,
				Pid:  pid,
				Args: map[string]any{
					"package":    key.Package,
					"incomplete": true,
				},
			})
		}
	}

	// Alphabetize unique packages to give them consistent thread IDs
	var pkgs []string
	for pkg := range uniquePackages {
		pkgs = append(pkgs, pkg)
	}
	sort.Strings(pkgs)

	pkgTids := make(map[string]int)
	for i, pkg := range pkgs {
		pkgTids[pkg] = i + 1
	}

	// Map tids to events
	for i := range events {
		pkg := events[i].Args["package"].(string)
		events[i].Tid = pkgTids[pkg]
	}

	// Append metadata events for thread names (packages)
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
func WriteTrace(resultsDir string, rep *Report) error {
	matches, err := filepath.Glob(filepath.Join(resultsDir, "iteration-*.log.jsonl"))
	if err != nil {
		return err
	}
	sort.Slice(matches, func(i, j int) bool {
		return iterNumber(matches[i]) < iterNumber(matches[j])
	})

	var allEvents []TraceEvent

	for _, path := range matches {
		iter := iterNumber(path)
		f, err := os.Open(path) //nolint:gosec // G304: path from filepath.Glob
		if err != nil {
			return err
		}
		events, err := parseTraceEvents(f, iter)
		_ = f.Close()
		if err != nil {
			return err
		}

		pid := iter + 1
		// Add process name metadata event
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

	// Write trace.json
	b, err := json.MarshalIndent(allEvents, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(resultsDir, "trace.json"), b, 0600)
}

// openBrowserFunc is a package-level variable to allow mocking browser launch in tests.
var openBrowserFunc = openBrowser

// ServeTrace starts a local HTTP server on port 9001 to serve trace.json, opens the browser to Perfetto UI, and blocks until ctx is cancelled.
func ServeTrace(ctx context.Context, resultsDir string, out *output.Printer) error {
	tracePath := filepath.Join(resultsDir, "trace.json")
	if _, err := os.Stat(tracePath); err != nil {
		return fmt.Errorf("trace file not found at %s: %w", tracePath, err)
	}

	listener, err := net.Listen("tcp", "127.0.0.1:9001")
	if err != nil {
		return fmt.Errorf(
			"failed to bind local port 9001 (is another trace server or perfetto instance running?): %w",
			err,
		)
	}
	defer func() { _ = listener.Close() }()

	mux := http.NewServeMux()
	mux.HandleFunc("/trace.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.Header().Set("Access-Control-Allow-Private-Network", "true")
		if r.Method == "OPTIONS" {
			return
		}
		http.ServeFile(w, r, tracePath)
	})

	server := &http.Server{
		Handler: mux,
	}

	go func() {
		_ = server.Serve(listener)
	}()

	perfettoURL := fmt.Sprintf("https://ui.perfetto.dev/#!/?url=http://%s/trace.json", listener.Addr().String())

	if out != nil {
		out.HumanStderr(fmt.Sprintf("\nServing trace at http://%s/trace.json\n", listener.Addr().String()))
		out.HumanStderr(fmt.Sprintf("Opening Perfetto UI at %s\n", perfettoURL))
		out.HumanStderr("Press Ctrl+C to stop serving...\n")
	} else {
		fmt.Printf("\nServing trace at http://%s/trace.json\n", listener.Addr().String())
		fmt.Printf("Opening Perfetto UI at %s\n", perfettoURL)
		fmt.Println("Press Ctrl+C to stop serving...")
	}

	_ = openBrowserFunc(perfettoURL)

	<-ctx.Done()
	if out != nil {
		out.HumanStderr("\nStopping server...\n")
	} else {
		fmt.Println("\nStopping server...")
	}
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
