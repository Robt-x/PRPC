package timewheel

import (
	"container/list"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"
)

type Timer struct {
	expiration int64
	task       func()
	duration   time.Duration
	periodic   bool
	slot       unsafe.Pointer
	element    *list.Element
}

func NewTimer(d time.Duration, f func(), periodic bool) *Timer {
	return &Timer{
		expiration: TimeToMs(time.Now().UTC().Add(d)),
		task:       f,
		duration:   d,
		periodic:   periodic,
	}
}

func (t *Timer) getBucket() *Slot {
	return (*Slot)(atomic.LoadPointer(&t.slot))
}

func (t *Timer) setBucket(b *Slot) {
	atomic.StorePointer(&t.slot, unsafe.Pointer(b))
}

func (t *Timer) stop() bool {
	stopped := false
	for b := t.getBucket(); b != nil; b = t.getBucket() {
		stopped = b.Remove(t)
	}
	return stopped
}

type Slot struct {
	expiration int64
	mu         sync.Mutex
	timers     *list.List
}

func newBucket() *Slot {
	return &Slot{
		expiration: -1,
		timers:     list.New(),
	}
}

func (s *Slot) Expiration() int64 {
	return atomic.LoadInt64(&s.expiration)
}

func (s *Slot) SetExpiration(expiration int64) bool {
	return atomic.SwapInt64(&s.expiration, expiration) != expiration
}

func (s *Slot) Add(t *Timer) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e := s.timers.PushBack(t)
	t.setBucket(s)
	t.element = e
}

func (s *Slot) remove(t *Timer) bool {
	if t.getBucket() != s {
		return false
	}
	s.timers.Remove(t.element)
	t.setBucket(nil)
	t.element = nil
	return true
}

func (s *Slot) Remove(t *Timer) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.remove(t)
}

func (s *Slot) Flush(reinsert func(*Timer)) {
	var ts []*Timer
	s.mu.Lock()
	for e := s.timers.Front(); e != nil; {
		next := e.Next()
		t := e.Value.(*Timer)
		s.remove(t)
		ts = append(ts, t)
		e = next
	}
	s.mu.Unlock()
	s.SetExpiration(-1)
	for _, t := range ts {
		reinsert(t)
	}
}
