package fail

import "testing"

func TestFail(t *testing.T) {
	t.Error("deliberate failure")
}
