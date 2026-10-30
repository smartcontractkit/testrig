package compare

import (
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFisherExact(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		a, b, c, d int
		want       float64
	}{
		{
			name: "scipy reference",
			a:    1, b: 9, c: 11, d: 3,
			want: 0.0027594561852200836,
		},
		{
			name: "flaky iterations 3 of 30 vs 1 of 30",
			a:    3, b: 27, c: 1, d: 29,
			want: 0.6119536128456735,
		},
		{
			name: "no problems in either group",
			a:    0, b: 10, c: 0, d: 10,
			want: 1,
		},
		{
			name: "empty group",
			a:    0, b: 10, c: 0, d: 0,
			want: 1,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.InDelta(t, tc.want, fisherExact(tc.a, tc.b, tc.c, tc.d), 1e-9)
		})
	}
}

func TestMannWhitneyPNoTies(t *testing.T) {
	t.Parallel()
	before := []time.Duration{1, 2, 3, 4, 5}
	after := []time.Duration{6, 7, 8, 9, 10}
	p, ok := mannWhitneyP(before, after)
	require.True(t, ok)
	// U = 0; two-sided exact p = 2 / C(10,5).
	assert.InDelta(t, 2.0/252.0, p, 1e-12)
}

func TestMannWhitneyPIdenticalSamples(t *testing.T) {
	t.Parallel()
	sample := []time.Duration{3, 1, 4, 1, 5}
	p, ok := mannWhitneyP(sample, sample)
	require.True(t, ok)
	assert.InDelta(t, 1, p, 1e-12)
}

func TestMannWhitneyPWithTies(t *testing.T) {
	t.Parallel()
	before := []time.Duration{1, 2, 2, 3, 5}
	after := []time.Duration{2, 3, 4, 5, 6}
	p, ok := mannWhitneyP(before, after)
	require.True(t, ok)
	assert.False(t, math.IsNaN(p))
	assert.GreaterOrEqual(t, p, 0.0)
	assert.LessOrEqual(t, p, 1.0)
}

func TestMannWhitneyPLargeSamplesFallBack(t *testing.T) {
	t.Parallel()
	before := make([]time.Duration, 200)
	after := make([]time.Duration, 200)
	for i := range before {
		before[i] = time.Duration(i)
		after[i] = time.Duration(i + 1000)
	}
	p, ok := mannWhitneyP(before, after)
	require.True(t, ok)
	assert.Less(t, p, 0.001)
}

func TestMannWhitneyPTooFewSamples(t *testing.T) {
	t.Parallel()
	_, ok := mannWhitneyP([]time.Duration{time.Second}, []time.Duration{2 * time.Second})
	assert.False(t, ok)
}

func TestFormatEvidence(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "clear", formatEvidence(0.0004))
	assert.Equal(t, "clear", formatEvidence(0.009658115943518335))
	assert.Equal(t, "weak", formatEvidence(0.02))
	assert.Equal(t, "unclear", formatEvidence(0.05))
	assert.Equal(t, "unclear", formatEvidence(1))
	assert.Equal(t, "—", formatEvidence(math.NaN()))
}
