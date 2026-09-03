// Copyright 2026 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package async

import (
	"log"
	"sync"
)

// Queue implements a concurrency-safe, generic FIFO queue using mutex and condition coordination.
type Queue[T any] struct {
	mu     sync.Mutex
	cond   *sync.Cond
	items  []T
	closed bool
}

// NewQueue creates and initializes a generic Queue.
func NewQueue[T any]() *Queue[T] {
	q := &Queue[T]{}
	q.cond = sync.NewCond(&q.mu)
	return q
}

// Push appends an item to the queue, waking up any waiting workers.
// Does nothing if the queue has been closed.
func (q *Queue[T]) Push(item T) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		log.Printf("warning: attempt to push to a closed queue")
		return
	}
	q.items = append(q.items, item)
	q.cond.Signal()
}

// Pop retrieves the first item from the queue, blocking until an item is available or the queue is closed.
// Returns (item, true) if an item was successfully popped.
// Returns (zero, false) if the queue is empty and closed.
func (q *Queue[T]) Pop() (T, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for len(q.items) == 0 && !q.closed {
		q.cond.Wait()
	}
	if len(q.items) > 0 {
		item := q.items[0]
		q.items = q.items[1:]
		return item, true
	}
	var zero T
	return zero, false
}

// Len returns the current number of items in the queue.
func (q *Queue[T]) Len() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.items)
}

// Close closes the queue and wakes up all waiting workers.
func (q *Queue[T]) Close() {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.closed = true
	q.cond.Broadcast()
}
