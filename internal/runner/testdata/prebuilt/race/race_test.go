package race

import (
	"sync"
	"testing"
)

func TestDataRace(t *testing.T) {
	var wg sync.WaitGroup
	value := 0
	wg.Add(2)
	go func() {
		defer wg.Done()
		value++
	}()
	go func() {
		defer wg.Done()
		value++
	}()
	wg.Wait()
}
