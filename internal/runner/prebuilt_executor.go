package runner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/smartcontractkit/testrig/internal/gobuild"
	"github.com/smartcontractkit/testrig/internal/test2json"
)

const (
	// prebuiltCancelGrace matches the runner's child-process WaitDelay: after
	// an interrupt, give a test binary five seconds to flush before killing it.
	prebuiltCancelGrace = 5 * time.Second

	// noTestsToRun mirrors cmd/go's warning from the testing package.
	noTestsToRun = "\ntesting: warning: no tests to run\n"
)

// prebuiltExecutor runs the test binaries linked during the build phase. Each
// binary gets its own test2json converter, so its events stay together, and at
// most P binaries run at once, like `go test`.
type prebuiltExecutor struct {
	binaries       []gobuild.Binary
	noTestPackages []gobuild.Package
	testFlags      []string
	passthrough    []string
	p              int
	// killTimeout is the backup deadline, timeout plus one minute (or a
	// larger share of long timeouts), matching cmd/go. Zero disables it.
	killTimeout time.Duration
}

func newPrebuiltExecutor(
	plan gobuild.BuildPlan,
	binaries []gobuild.Binary,
	noTestPackages []gobuild.Package,
) *prebuiltExecutor {
	e := &prebuiltExecutor{
		binaries:       binaries,
		noTestPackages: noTestPackages,
		testFlags:      append([]string(nil), plan.TestFlags...),
		passthrough:    append([]string(nil), plan.Passthrough...),
		p:              max(plan.P, 1),
	}
	if timeout, ok := testTimeoutFromFlags(e.testFlags); ok && timeout > 0 && !hasBenchOrFuzz(e.testFlags) {
		e.killTimeout = testKillTimeout(timeout)
	}
	return e
}

// Run executes every package once. Packages without test files get the same
// events cmd/go emits, without starting a process. Finished packages flush
// their buffer to w under a lock, so one package's events never interleave
// with another's.
func (e *prebuiltExecutor) Run(ctx context.Context, it iterationRun, w io.Writer) error {
	jobs := e.jobs()
	if len(jobs) == 0 {
		return nil
	}

	sem := make(chan struct{}, e.p)
	var wg sync.WaitGroup
	var writeMu sync.Mutex
	var errMu sync.Mutex
	var firstErr error
	recordErr := func(err error) {
		if err == nil {
			return
		}
		errMu.Lock()
		if firstErr == nil {
			firstErr = err
		}
		errMu.Unlock()
	}

	for _, job := range jobs {
		if ctx.Err() != nil {
			break
		}
		acquired := false
		select {
		case <-ctx.Done():
		case sem <- struct{}{}:
			acquired = true
		}
		if !acquired {
			break
		}
		wg.Go(func() {
			defer func() { <-sem }()
			var buf bytes.Buffer
			err := e.runJob(ctx, job, it, &buf)
			writeMu.Lock()
			_, _ = w.Write(buf.Bytes())
			writeMu.Unlock()
			recordErr(err)
		})
	}
	wg.Wait()
	if firstErr != nil {
		return firstErr
	}
	return ctx.Err()
}

type prebuiltJob struct {
	hasBinary bool
	binary    gobuild.Binary
	noTests   gobuild.Package
}

func (e *prebuiltExecutor) jobs() []prebuiltJob {
	jobs := make([]prebuiltJob, 0, len(e.binaries)+len(e.noTestPackages))
	for _, binary := range e.binaries {
		jobs = append(jobs, prebuiltJob{hasBinary: true, binary: binary})
	}
	for _, pkg := range e.noTestPackages {
		jobs = append(jobs, prebuiltJob{noTests: pkg})
	}
	return jobs
}

func (e *prebuiltExecutor) runJob(ctx context.Context, job prebuiltJob, it iterationRun, buf *bytes.Buffer) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if !job.hasBinary {
		conv := test2json.NewConverter(buf, job.noTests.ImportPath, test2json.Timestamp)
		_, _ = fmt.Fprintf(conv, "?   \t%s\t[no test files]\n", job.noTests.ImportPath)
		conv.Exited(nil)
		return conv.Close()
	}
	return e.runBinary(ctx, job.binary, it, buf)
}

func (e *prebuiltExecutor) runBinary(
	ctx context.Context,
	binary gobuild.Binary,
	it iterationRun,
	buf *bytes.Buffer,
) error {
	conv := test2json.NewConverter(buf, binary.ImportPath, test2json.Timestamp)

	var raw bytes.Buffer
	// Stdout and Stderr share one pointer so os/exec uses a single pipe and
	// the converter never sees concurrent writes.
	output := &syncedWriter{w: io.MultiWriter(conv, &raw)}
	args := append(e.iterationTestFlags(it.ShuffleSeed), e.passthrough...)

	//nolint:gosec // G204: the binary path and arguments come from the build
	cmd := exec.Command(binary.Path, args...)
	cmd.Dir = binary.Dir
	cmd.Env = append(os.Environ(), it.Env...)
	cmd.Env = append(cmd.Env, "PWD="+binary.Dir)
	cmd.Stdout = output
	cmd.Stderr = output
	cmd.WaitDelay = prebuiltCancelGrace
	isolateDiagnoseChildProcessGroup(cmd)

	start := time.Now()
	killedTooLong, runErr := runPrebuiltCommand(ctx, cmd, e.killTimeout)
	elapsed := time.Since(start)

	writePrebuiltFinish(conv, binary.ImportPath, raw.Bytes(), runErr, killedTooLong, elapsed, e.killTimeout)
	conv.Exited(runErr)
	_ = conv.Close()
	if runErr == nil && ctx.Err() != nil {
		return ctx.Err()
	}
	return runErr
}

// iterationTestFlags returns the binary flags for one iteration. A non-zero
// harness shuffle seed replaces a user -shuffle value, so report.json and the
// binary agree on the seed.
func (e *prebuiltExecutor) iterationTestFlags(seed int64) []string {
	if seed == 0 {
		return append([]string(nil), e.testFlags...)
	}
	flags := make([]string, 0, len(e.testFlags)+1)
	for _, flag := range e.testFlags {
		if strings.HasPrefix(flag, "-test.shuffle=") {
			continue
		}
		flags = append(flags, flag)
	}
	return append(flags, fmt.Sprintf("-test.shuffle=%d", seed))
}

// runPrebuiltCommand starts cmd and waits for it, enforcing the backup kill
// deadline. On ctx cancellation it interrupts the process group, waits five
// seconds for buffered output, then kills the group.
func runPrebuiltCommand(
	ctx context.Context,
	cmd *exec.Cmd,
	killTimeout time.Duration,
) (killedTooLong bool, err error) {
	if ctx.Err() != nil {
		return false, ctx.Err()
	}
	if err := cmd.Start(); err != nil {
		return false, err
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	var timeoutC <-chan time.Time
	if killTimeout > 0 {
		timer := time.NewTimer(killTimeout)
		defer timer.Stop()
		timeoutC = timer.C
	}

	select {
	case err := <-done:
		return false, err
	case <-ctx.Done():
		_ = interruptDiagnoseChildProcessGroup(cmd)
		return false, waitForPrebuiltExit(cmd, done)
	case <-timeoutC:
		_ = killDiagnoseChildProcessGroup(cmd)
		return true, <-done
	}
}

// waitForPrebuiltExit waits for an interrupted process, then kills its group
// if it ignores the interrupt.
func waitForPrebuiltExit(cmd *exec.Cmd, done <-chan error) error {
	timer := time.NewTimer(prebuiltCancelGrace)
	defer timer.Stop()
	select {
	case err := <-done:
		return err
	case <-timer.C:
		_ = killDiagnoseChildProcessGroup(cmd)
		return <-done
	}
}

// writePrebuiltFinish writes the line cmd/go writes after a test binary exits,
// so the converter can emit the package's final event with its elapsed time.
func writePrebuiltFinish(
	conv *test2json.Converter,
	importPath string,
	out []byte,
	runErr error,
	killedTooLong bool,
	elapsed time.Duration,
	killTimeout time.Duration,
) {
	if runErr == nil {
		norun := ""
		if bytes.HasPrefix(out, []byte(noTestsToRun[1:])) || bytes.Contains(out, []byte(noTestsToRun)) {
			norun = " [no tests to run]"
		}
		if len(out) > 0 && !bytes.HasSuffix(out, []byte("\n")) {
			_, _ = conv.Write([]byte("\n"))
		}
		_, _ = fmt.Fprintf(conv, "ok  \t%s\t%.3fs%s\n", importPath, elapsed.Seconds(), norun)
		return
	}

	switch {
	case killedTooLong:
		_, _ = fmt.Fprintf(conv, "*** Test killed: ran too long (%v).\n", killTimeout)
	case errors.Is(runErr, exec.ErrWaitDelay):
		_, _ = fmt.Fprintf(conv, "*** Test I/O incomplete %v after exiting.\n", prebuiltCancelGrace)
	}
	var exitErr *exec.ExitError
	if !errors.As(runErr, &exitErr) || !exitErr.Exited() || len(out) == 0 {
		// No test output means the exit status is the only clue.
		_, _ = fmt.Fprintf(conv, "%s\n", runErr)
	} else if !bytes.HasSuffix(out, []byte("\n")) {
		_, _ = conv.Write([]byte("\n"))
	}
	// The ^V marker makes the converter treat FAIL as framing, matching
	// cmd/go when -json is on.
	_, _ = fmt.Fprintf(conv, "\x16FAIL\t%s\t%.3fs\n", importPath, elapsed.Seconds())
}

// testTimeoutFromFlags returns the last -test.timeout value a binary receives.
func testTimeoutFromFlags(flags []string) (time.Duration, bool) {
	var raw string
	found := false
	for _, flag := range flags {
		if value, ok := strings.CutPrefix(flag, "-test.timeout="); ok {
			raw = value
			found = true
		}
	}
	if !found {
		return 0, false
	}
	timeout, err := time.ParseDuration(raw)
	if err != nil {
		return 0, false
	}
	return timeout, true
}

// hasBenchOrFuzz reports whether the binary may run past -test.timeout, which
// disables cmd/go's backup kill deadline.
func hasBenchOrFuzz(flags []string) bool {
	for _, flag := range flags {
		if strings.HasPrefix(flag, "-test.bench=") ||
			strings.HasPrefix(flag, "-test.fuzz=") ||
			strings.HasPrefix(flag, "-test.fuzztime=") {
			return true
		}
	}
	return false
}

// testKillTimeout mirrors cmd/go's backup alarm: timeout plus one minute, or
// timeout plus its I/O grace for long runs.
func testKillTimeout(timeout time.Duration) time.Duration {
	grace := timeout / 10
	if grace < time.Minute {
		return timeout + time.Minute
	}
	return timeout + grace
}
