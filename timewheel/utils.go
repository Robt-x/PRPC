package timewheel

import (
	"sync"
	"time"
)

func TimeToMs(t time.Time) int64 {
	return t.UnixNano() / int64(time.Millisecond)
}

func truncate(x, m int64) int64 {
	if m <= 0 {
		return x
	}
	return x - x%m
}

type waitGroupWrapper struct {
	sync.WaitGroup
}

func (w *waitGroupWrapper) Wrap(g func()) {
	w.Add(1)
	go func() {
		g()
		w.Done()
	}()
}
