package queue

import (
	"container/heap"
	"sync"
	"sync/atomic"
	"time"
)

type Item struct {
	Value    interface{}
	Priority int64 //过期时间
	Index    int
}

type priorityQueue []*Item

func newPriorityQueue(capacity int) priorityQueue {
	return make(priorityQueue, 0, capacity)
}

func (pq *priorityQueue) Len() int {
	return len(*pq)
}

func (pq *priorityQueue) Less(i, j int) bool {
	return (*pq)[i].Priority < (*pq)[j].Priority
}

func (pq *priorityQueue) Swap(i, j int) {
	(*pq)[i], (*pq)[j] = (*pq)[j], (*pq)[i]
	(*pq)[i].Index = i
	(*pq)[j].Index = j
}

func (pq *priorityQueue) Push(x interface{}) {
	n := len(*pq)
	c := cap(*pq)
	if n+1 > c {
		npq := make(priorityQueue, n, c*2)
		copy(npq, *pq)
		*pq = npq
	}
	*pq = (*pq)[0 : n+1]
	item := x.(*Item)
	item.Index = n
	(*pq)[n] = item
}

func (pq *priorityQueue) Pop() interface{} {
	n := len(*pq)
	c := cap(*pq)
	if n < (c/2) && c > 25 {
		npq := make(priorityQueue, n, c/2)
		copy(npq, *pq)
		*pq = npq
	}
	item := (*pq)[n-1]
	item.Index = -1
	*pq = (*pq)[0 : n-1]
	return item
}

func (pq *priorityQueue) ShiftAndPeak(now int64) (*Item, int64) {
	if pq.Len() == 0 {
		return nil, 0
	}
	item := (*pq)[0]
	if item.Priority > now {
		return nil, item.Priority - now
	}
	heap.Remove(pq, 0)
	return item, 0
}

type DelayQueue struct {
	mu       sync.Mutex
	C        chan interface{}
	pq       priorityQueue
	sleeping int32
	wakeupC  chan struct{}
}

func NewDelayQueue(size int) *DelayQueue {
	return &DelayQueue{
		C:       make(chan interface{}),
		pq:      newPriorityQueue(size),
		wakeupC: make(chan struct{}),
	}
}

func (dq *DelayQueue) Offer(elem interface{}, expiration int64) {
	item := &Item{
		Value:    elem,
		Priority: expiration,
	}
	dq.mu.Lock()
	heap.Push(&dq.pq, item)
	index := item.Index
	dq.mu.Unlock()
	if index == 0 {
		if atomic.CompareAndSwapInt32(&dq.sleeping, 1, 0) {
			dq.wakeupC <- struct{}{}
		}
	}
}

func (dq *DelayQueue) Poll(exitC chan struct{}, nowF func() int64) {
	for {
		now := nowF()
		dq.mu.Lock()
		item, delta := dq.pq.ShiftAndPeak(now)
		if item == nil {
			//没有剩余item或存在item未过期
			atomic.StoreInt32(&dq.sleeping, 1)
		}
		dq.mu.Unlock()
		if item == nil {
			if delta == 0 {
				//没有剩余item
				select {
				case <-dq.wakeupC:
					continue
				case <-exitC:
					goto exit
				}
			} else if delta > 0 {
				//item未到期
				select {
				case <-dq.wakeupC: //新加入了到期的item
					continue
				case <-time.After(time.Duration(delta) * time.Millisecond):
					//item到期开始执行到改变sleeping状态这段时间里又加入了可执行的item
					if atomic.SwapInt32(&dq.sleeping, 0) == 0 {
						<-dq.wakeupC
					}
					continue
				case <-exitC:
					goto exit
				}
			}
		}
		select {
		case dq.C <- item.Value:
		case <-exitC:
			goto exit
		}
	}
exit:
	atomic.StoreInt32(&dq.sleeping, 0)
}
