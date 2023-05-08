package timewheel

import (
	"PRPC/logger"
	"PRPC/timewheel/queue"
	"errors"
	"sync/atomic"
	"time"
	"unsafe"
)

type TimeWheel struct {
	//时间跨度
	tick int64 //ms
	//时间轮个数
	slotNum int64
	//总跨度
	interval      int64
	currentTime   int64
	slots         []*Slot
	Ticker        *queue.DelayQueue
	overflowWheel unsafe.Pointer
	exitC         chan struct{}
	waitGroup     waitGroupWrapper
}

func NewTimeWheel(tick time.Duration, wheelSize int64) *TimeWheel {
	tickMs := int64(tick / time.Millisecond)
	if tickMs <= 0 {
		err := errors.New("tick must be greater than or equal to 1ms")
		logger.Error(err)
		return nil
	}
	starMs := TimeToMs(time.Now().UTC())
	return newTimeWheel(
		tickMs,
		wheelSize,
		starMs,
		queue.NewDelayQueue(int(wheelSize)),
	)
}

func newTimeWheel(tickMs int64, wheelSize int64, starMs int64, queue *queue.DelayQueue) *TimeWheel {
	buckets := make([]*Slot, wheelSize)
	for i := range buckets {
		buckets[i] = newBucket()
	}
	return &TimeWheel{
		tick:        tickMs,
		slotNum:     wheelSize,
		interval:    tickMs * wheelSize,
		currentTime: truncate(starMs, tickMs),
		slots:       buckets,
		Ticker:      queue,
		exitC:       make(chan struct{}),
	}
}

func (tw *TimeWheel) Start() {
	tw.waitGroup.Wrap(func() {
		tw.Ticker.Poll(tw.exitC, func() int64 {
			return TimeToMs(time.Now().UTC())
		})
	})
	tw.waitGroup.Wrap(func() {
		for {
			select {
			case elem := <-tw.Ticker.C:
				b := elem.(*Slot)
				tw.advanceClock(b.Expiration())
				b.Flush(tw.addOrRun)
			case <-tw.exitC:
				return
			}
		}
	})
}

func (tw *TimeWheel) Stop() {
	close(tw.exitC)
	tw.waitGroup.Wait()
}

func (tw *TimeWheel) AddTask(d time.Duration, f func(), periodic bool) *Timer {
	t := NewTimer(d, f, periodic)
	tw.addOrRun(t)
	return t
}

func (tw *TimeWheel) AddTaskByTimer(t *Timer) {
	t.expiration = TimeToMs(time.Now().UTC().Add(t.duration))
	tw.add(t)
}

func (tw *TimeWheel) RemoveTask(t *Timer) bool {
	return t.getBucket().Remove(t)
}

func (tw *TimeWheel) advanceClock(expiration int64) {
	currentTime := atomic.LoadInt64(&tw.currentTime)
	if expiration >= currentTime+tw.tick {
		currentTime = truncate(expiration, tw.tick)
		atomic.StoreInt64(&tw.currentTime, currentTime)
		overflowWheel := atomic.LoadPointer(&tw.overflowWheel)
		if overflowWheel != nil {
			(*TimeWheel)(overflowWheel).advanceClock(currentTime)
		}
	}
}

func (tw *TimeWheel) addOrRun(t *Timer) {
	if !tw.add(t) {
		if t.periodic {
			tw.AddTaskByTimer(t)
		}
		go t.task()
	}
}

func (tw *TimeWheel) add(t *Timer) bool {
	currentTime := atomic.LoadInt64(&tw.currentTime)
	if t.expiration < currentTime+tw.tick {
		return false
	} else if t.expiration < currentTime+tw.interval {
		virtualID := t.expiration / tw.tick
		b := tw.slots[virtualID%tw.slotNum]
		b.Add(t)
		if b.SetExpiration(virtualID * tw.tick) {
			tw.Ticker.Offer(b, b.Expiration())
		}
		return true
	} else {
		overflowWheel := atomic.LoadPointer(&tw.overflowWheel)
		if overflowWheel == nil {
			atomic.CompareAndSwapPointer(
				&tw.overflowWheel,
				nil,
				unsafe.Pointer(newTimeWheel(
					tw.interval,
					tw.slotNum,
					currentTime,
					tw.Ticker,
				)),
			)
			overflowWheel = atomic.LoadPointer(&tw.overflowWheel)
		}
		return (*TimeWheel)(overflowWheel).add(t)
	}
}
