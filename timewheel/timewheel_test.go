package timewheel

import (
	"fmt"
	"testing"
	"time"
)

func TestTime(t *testing.T) {
	tw := NewTimeWheel(time.Second, 10)
	tw.Start()
	defer tw.Stop()
	d := time.Second
	start := time.Now().UTC().Truncate(time.Second)
	fmt.Println(start)
	tm := tw.AddTask(d, func() {
		got := time.Now().UTC().Truncate(time.Second)
		fmt.Println(got)
	}, true)
	<-time.After(5 * time.Second)
	tw.RemoveTask(tm)
	<-time.After(5 * time.Second)
}
