package timeout

import (
	"testing"
	"time"
)

func TestHangs(t *testing.T) {
	time.Sleep(30 * time.Second)
}
