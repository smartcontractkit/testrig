package args

import (
	"os"
	"slices"
	"testing"
)

func TestArgsReachBinary(t *testing.T) {
	if !slices.Contains(os.Args, "hello") {
		t.Fatalf("arguments after -args did not reach the binary: %v", os.Args)
	}
}
