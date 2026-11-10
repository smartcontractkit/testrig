package compare

import (
	"math"
	"sort"
	"time"
)

// Evidence thresholds for the summary table. Raw p-values stay internal: the
// report shows words so readers are not asked to interpret a number.
const (
	evidenceClearP = 0.01
	evidenceWeakP  = 0.05
)

// formatEvidence turns a p-value into a short, stable token for humans and
// agents. "unclear" means the test did not reach the weak threshold; it does not
// claim there is no effect.
func formatEvidence(p float64) string {
	switch {
	case math.IsNaN(p):
		return "—"
	case p < evidenceClearP:
		return "clear"
	case p < evidenceWeakP:
		return "weak"
	default:
		return "unclear"
	}
}

// fisherExact returns the two-sided Fisher's exact test p-value for the 2x2
// table [[a, b], [c, d]]:
//
//	         before  after
//	flagged     a      c
//	ok          b      d
//
// It sums the hypergeometric probabilities of all tables with the same margins
// whose probability is no larger than the observed table's, which is the
// two-sided definition used by scipy.stats.fisher_exact.
func fisherExact(a, b, c, d int) float64 {
	if a < 0 || b < 0 || c < 0 || d < 0 {
		return 1
	}
	row1, row2 := a+b, c+d
	col1, col2 := a+c, b+d
	n := row1 + row2
	if n == 0 || row1 == 0 || row2 == 0 || col1 == 0 || col2 == 0 {
		return 1
	}

	lo := max(0, col1-row2)
	hi := min(row1, col1)
	observed := logHypergeom(row1, row2, col1, n, a)
	probabilities := make([]float64, 0, hi-lo+1)
	maxLog := math.Inf(-1)
	for x := lo; x <= hi; x++ {
		logP := logHypergeom(row1, row2, col1, n, x)
		probabilities = append(probabilities, logP)
		maxLog = max(maxLog, logP)
	}

	// Scale by the largest probability so exp never underflows, then normalize
	// by the total, which is exp(-maxLog).
	threshold := math.Exp(observed-maxLog) * (1 + 1e-7)
	var total, tail float64
	for _, logP := range probabilities {
		relative := math.Exp(logP - maxLog)
		total += relative
		if relative <= threshold {
			tail += relative
		}
	}
	if total == 0 {
		return 1
	}
	return math.Min(1, tail/total)
}

func logHypergeom(row1, row2, col1, n, x int) float64 {
	// x cells come from row1, col1-x from row2, both landing in the first column.
	return logChoose(row1, x) + logChoose(row2, col1-x) - logChoose(n, col1)
}

func logChoose(n, k int) float64 {
	if k < 0 || k > n {
		return math.Inf(-1)
	}
	return logGamma(float64(n)+1) - logGamma(float64(k)+1) - logGamma(float64(n-k)+1)
}

func logGamma(x float64) float64 {
	v, _ := math.Lgamma(x)
	return v
}

type rankedValue struct {
	value  int64 // time.Duration as nanoseconds
	before bool
}

// mannWhitneyP returns the two-sided p-value for a location shift between two
// samples. It uses the exact U distribution when there are no ties and a normal
// approximation with tie correction otherwise. ok is false when either sample is
// too small to test.
func mannWhitneyP(before, after []time.Duration) (p float64, ok bool) {
	n1, n2 := len(before), len(after)
	if n1 < 2 || n2 < 2 {
		return 0, false
	}
	combined := make([]rankedValue, 0, n1+n2)
	for _, d := range before {
		combined = append(combined, rankedValue{value: int64(d), before: true})
	}
	for _, d := range after {
		combined = append(combined, rankedValue{value: int64(d)})
	}
	sort.SliceStable(combined, func(i, j int) bool { return combined[i].value < combined[j].value })

	rankSum := 0.0
	hasTies := false
	for i := 0; i < len(combined); {
		j := i
		for j+1 < len(combined) && combined[j+1].value == combined[i].value {
			j++
		}
		midrank := float64(i+j)/2 + 1
		if j > i {
			hasTies = true
		}
		for k := i; k <= j; k++ {
			if combined[k].before {
				rankSum += midrank
			}
		}
		i = j + 1
	}

	u := rankSum - float64(n1*(n1+1))/2
	if !hasTies && exactMannWhitneyFeasible(n1, n2) {
		return exactMannWhitneyP(u, n1, n2), true
	}
	return mannWhitneyNormalP(u, n1, n2, combined), true
}

// exactMannWhitneyFeasible bounds the DP table and work. Diagnose iteration
// counts are usually small; long runs fall back to the normal approximation.
func exactMannWhitneyFeasible(n1, n2 int) bool {
	n := n1 + n2
	maxSum := n * (n + 1) / 2
	if maxSum*(n1+1) > 2_000_000 {
		return false
	}
	return n*n1*maxSum <= 50_000_000
}

// exactMannWhitneyP enumerates the null distribution of U by counting subsets of
// ranks. Counts stay below ~1.2e17 for realistic iteration counts, well within
// float64's range.
func exactMannWhitneyP(u float64, n1, n2 int) float64 {
	n := n1 + n2
	maxSum := n * (n + 1) / 2
	counts := make([][]float64, n1+1)
	for i := range counts {
		counts[i] = make([]float64, maxSum+1)
	}
	counts[0][0] = 1
	for rank := 1; rank <= n; rank++ {
		for chosen := min(rank, n1); chosen >= 1; chosen-- {
			for sum := maxSum; sum >= rank; sum-- {
				counts[chosen][sum] += counts[chosen-1][sum-rank]
			}
		}
	}

	observed := int(math.Round(u))
	base := n1 * (n1 + 1) / 2
	var total, lower, upper float64
	for sum, count := range counts[n1] {
		total += count
		uValue := sum - base
		if uValue <= observed {
			lower += count
		}
		if uValue >= observed {
			upper += count
		}
	}
	if total == 0 {
		return 1
	}
	return math.Min(1, 2*math.Min(lower, upper)/total)
}

func mannWhitneyNormalP(u float64, n1, n2 int, combined []rankedValue) float64 {
	n := float64(n1 + n2)
	mean := float64(n1*n2) / 2
	var tieSum float64
	for i := 0; i < len(combined); {
		j := i
		for j+1 < len(combined) && combined[j+1].value == combined[i].value {
			j++
		}
		size := float64(j - i + 1)
		if size > 1 {
			tieSum += size*size*size - size
		}
		i = j + 1
	}
	variance := float64(n1*n2) / 12 * ((n + 1) - tieSum/(n*(n-1)))
	if variance <= 0 {
		return 1
	}
	// Continuity correction, clamped at zero.
	z := (math.Abs(u-mean) - 0.5) / math.Sqrt(variance)
	if z < 0 {
		z = 0
	}
	return math.Min(1, math.Erfc(z/math.Sqrt2))
}
