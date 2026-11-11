package testdata

import (
	"os"
	"testing"
)

func TestReadsRelativeTestdata(t *testing.T) {
	got, err := os.ReadFile("testdata/value.txt")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "relative path works\n" {
		t.Fatalf("unexpected value %q", got)
	}
}
