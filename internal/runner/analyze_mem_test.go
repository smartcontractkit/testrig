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

type mockJSONLReader struct {
	iteration int
	testIdx   int
	numTests  int
	buf       *bytes.Buffer
}

func newMockJSONLReader(iteration, numTests int) *mockJSONLReader {
	return &mockJSONLReader{
		iteration: iteration,
		numTests:  numTests,
		buf:       bytes.NewBuffer(nil),
	}
}

func (m *mockJSONLReader) Read(p []byte) (n int, err error) {
	if m.buf.Len() == 0 {
		if m.testIdx >= m.numTests {
			return 0, io.EOF
		}
		// generate tests
		for i := 0; i < 50 && m.testIdx < m.numTests; i++ {
			pkg := fmt.Sprintf("pkg/example%d", m.testIdx%50)
			test := fmt.Sprintf("TestSomething%d", m.testIdx)

			// Generate a run and pass event
			run := fmt.Sprintf(`{"Action":"run","Package":%q,"Test":%q}`+"\n", pkg, test)
			pass := fmt.Sprintf(`{"Action":"pass","Package":%q,"Test":%q,"Elapsed":0.01}`+"\n", pkg, test)
			m.buf.WriteString(run)
			m.buf.WriteString(pass)
			m.testIdx++
		}
	}
	return m.buf.Read(p)
}

//nolint:paralleltest // serial: memory stats need stable measurement
func TestAnalyzeMemory_Limit(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping memory test in short mode")
	}
	numIters := 250
	numTests := 8500 // roughly chainlink scale

	readers := make([]io.Reader, numIters)
	for i := range numIters {
		readers[i] = newMockJSONLReader(i, numTests)
	}

	runtime.GC()
	var memBefore runtime.MemStats
	runtime.ReadMemStats(&memBefore)

	done := make(chan struct{})
	var maxAlloc uint64
	go func() {
		for {
			select {
			case <-done:
				return
			case <-time.After(1 * time.Millisecond):
				var mem runtime.MemStats
				runtime.ReadMemStats(&mem)
				if mem.Alloc > maxAlloc {
					maxAlloc = mem.Alloc
				}
			}
		}
	}()

	rep, _, err := Analyze(readers, 30*time.Second)
	close(done)

	require.NoError(t, err)
	require.NotNil(t, rep)
	assert.Len(t, rep.IterationSummaries, numIters)

	peakAllocBytes := maxAlloc - memBefore.Alloc
	allocMB := float64(peakAllocBytes) / 1024 / 1024

	t.Logf("Analyze peak HeapAlloc was %.2f MB for %d tests across %d iterations", allocMB, numTests, numIters)

	// Set a strict threshold that will fail with the map-based approach
	maxMB := 50.0
	assert.Less(t, allocMB, maxMB, "Memory usage exceeded threshold")
}
