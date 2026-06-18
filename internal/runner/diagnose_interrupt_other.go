//go:build !unix

package runner

func isInterruptedIteration(iterErr error) bool {
	return false
}
