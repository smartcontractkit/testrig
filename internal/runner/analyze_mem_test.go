package runner

import (
	"bytes"
	"fmt"
	"io"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

//nolint:paralleltest // serial: memory stats need stable measurement
func TestAnalyzeMemory_Limit(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping memory test in short mode")
	}
	const (
		numIters = 250
		numTests = 8500 // roughly chainlink scale
	)

	var buf bytes.Buffer
	for i := range numTests {
		pkg := fmt.Sprintf("pkg/example%d", i%50)
		test := fmt.Sprintf("TestSomething%d", i)
		fmt.Fprintf(&buf, `{"Action":"run","Package":%q,"Test":%q}`+"\n", pkg, test)
		fmt.Fprintf(&buf, `{"Action":"pass","Package":%q,"Test":%q,"Elapsed":0.01}`+"\n", pkg, test)
	}
	precomputed := buf.Bytes()

	readers := make([]io.Reader, numIters)
	for i := range numIters {
		readers[i] = bytes.NewReader(precomputed)
	}

	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)

	rep, _, err := Analyze(readers, 30*time.Second)
	require.NoError(t, err)

	runtime.ReadMemStats(&after)

	require.NotNil(t, rep)
	assert.Len(t, rep.IterationSummaries, numIters)

	allocBytes := after.TotalAlloc - before.TotalAlloc
	allocMB := float64(allocBytes) / 1024 / 1024

	t.Logf("Analyze total allocated was %.2f MB for %d tests across %d iterations", allocMB, numTests, numIters)

	// A realistic TotalAlloc bound for parsing 2.1 million JSON test events.
	const maxAllocMB = 1800.0
	assert.Less(t, allocMB, maxAllocMB, "total allocations exceeded threshold")
}
