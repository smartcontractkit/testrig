package pass

import "testing"

func TestPass(t *testing.T) {
	t.Log("pass output")
}

func TestSkipped(t *testing.T) {
	t.Skip("skip reason")
}

func TestSubtests(t *testing.T) {
	t.Run("one", func(t *testing.T) {
		t.Log("first subtest")
	})
	t.Run("two", func(t *testing.T) {
		t.Log("second subtest")
	})
}

func TestParallel(t *testing.T) {
	t.Run("first", func(t *testing.T) {
		t.Parallel()
		t.Log("parallel output")
	})
	t.Run("second", func(t *testing.T) {
		t.Parallel()
		t.Log("parallel output")
	})
}
