package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/smartcontractkit/testrig/internal/gobuild"
	"github.com/smartcontractkit/testrig/internal/test2json"
)

func TestPrebuiltIterationTestFlags(t *testing.T) {
	t.Parallel()

	e := &prebuiltExecutor{
		testFlags: []string{"-test.run=TestX", "-test.shuffle=1234", "-test.count=1"},
	}

	assert.Equal(t, e.testFlags, e.iterationTestFlags(0), "no harness seed preserves user flags")
	assert.Equal(
		t,
		[]string{"-test.run=TestX", "-test.count=1", "-test.shuffle=42"},
		e.iterationTestFlags(42),
		"harness seed replaces the user shuffle value",
	)
}

func TestTestKillTimeout(t *testing.T) {
	t.Parallel()

	assert.Equal(t, time.Minute+time.Second, testKillTimeout(time.Second))
	assert.Equal(t, 11*time.Minute, testKillTimeout(10*time.Minute))
	assert.Equal(t, 33*time.Minute, testKillTimeout(30*time.Minute))
}

func TestTestTimeoutFromFlags(t *testing.T) {
	t.Parallel()

	timeout, ok := testTimeoutFromFlags([]string{"-test.timeout=1s", "-test.run=TestX"})
	require.True(t, ok)
	assert.Equal(t, time.Second, timeout)

	timeout, ok = testTimeoutFromFlags([]string{"-test.timeout=30s", "-test.timeout=2s"})
	require.True(t, ok)
	assert.Equal(t, 2*time.Second, timeout, "last value wins, like flag parsing")

	_, ok = testTimeoutFromFlags([]string{"-test.run=TestX"})
	assert.False(t, ok)
}

func TestNewPrebuiltExecutorBenchSerial(t *testing.T) {
	t.Parallel()

	bench := newPrebuiltExecutor(
		gobuild.BuildPlan{P: 8, TestFlags: []string{"-test.bench=.", "-test.count=1"}},
		nil,
		nil,
	)
	assert.Equal(t, 1, bench.p, "cmd/go runs benchmark binaries one at a time")

	plain := newPrebuiltExecutor(
		gobuild.BuildPlan{P: 8, TestFlags: []string{"-test.run=TestX"}},
		nil,
		nil,
	)
	assert.Equal(t, 8, plain.p)
}

func TestHasBenchOrFuzz(t *testing.T) {
	t.Parallel()

	assert.False(t, hasBenchOrFuzz([]string{"-test.run=TestX"}))
	assert.True(t, hasBenchOrFuzz([]string{"-test.bench=."}))
	assert.True(t, hasBenchOrFuzz([]string{"-test.fuzz=FuzzX"}))
}

func TestPrebuiltNoTestFilesEvents(t *testing.T) {
	t.Parallel()

	e := &prebuiltExecutor{
		noTestPackages: []gobuild.Package{{ImportPath: "example.com/empty"}},
		p:              1,
	}
	var buf bytes.Buffer
	require.NoError(t, e.Run(context.Background(), iterationRun{}, &buf))

	events := decodePrebuiltEvents(t, buf.String())
	require.Len(t, events, 3)
	assert.Equal(t, "start", events[0].Action)
	assert.Equal(t, "?   \texample.com/empty\t[no test files]\n", events[1].Output)
	assert.Equal(t, "skip", events[2].Action)
}

func TestWritePrebuiltFinish(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		out           string
		err           error
		killedTooLong bool
		want          []string
	}{
		{
			name: "pass",
			out:  "PASS\n",
			want: []string{"ok  \texample.com/p\t0.123s\n"},
		},
		{
			name: "pass with no tests to run",
			out:  "testing: warning: no tests to run\nPASS\n",
			want: []string{"ok  \texample.com/p\t0.123s [no tests to run]\n"},
		},
		{
			name: "failure without trailing newline",
			out:  "custom failure output",
			err:  errors.New("exit status 1"),
			want: []string{"exit status 1\n", "FAIL\texample.com/p\t0.123s\n"},
		},
		{
			name: "failure with no output",
			out:  "",
			err:  errors.New("exit status 1"),
			want: []string{"exit status 1\n", "FAIL\texample.com/p\t0.123s\n"},
		},
		{
			name:          "killed too long",
			out:           "partial\n",
			err:           errors.New("signal: killed"),
			killedTooLong: true,
			want: []string{
				"*** Test killed: ran too long (1m0s).\n",
				"signal: killed\n",
				"FAIL\texample.com/p\t0.123s\n",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			var buf bytes.Buffer
			conv := test2json.NewConverter(&buf, "example.com/p", 0)
			writePrebuiltFinish(
				conv,
				"example.com/p",
				[]byte(test.out),
				test.err,
				test.killedTooLong,
				123*time.Millisecond,
				time.Minute,
			)
			require.NoError(t, conv.Close())
			assert.Equal(t, test.want, outputEventTexts(t, buf.String()))
		})
	}
}

func decodePrebuiltEvents(t *testing.T, stream string) []TestEvent {
	t.Helper()
	var events []TestEvent
	for line := range bytes.SplitSeq([]byte(stream), []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		var event TestEvent
		require.NoError(t, json.Unmarshal(line, &event))
		events = append(events, event)
	}
	return events
}

func outputEventTexts(t *testing.T, stream string) []string {
	t.Helper()
	var out []string
	for _, event := range decodePrebuiltEvents(t, stream) {
		if event.Action == "output" {
			out = append(out, event.Output)
		}
	}
	return out
}
