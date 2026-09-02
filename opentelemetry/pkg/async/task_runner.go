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
	"errors"
	"io"
	"sync"
	"sync/atomic"
)

var (
	// ErrStopped is returned when submitting tasks to a stopped TaskRunner.
	ErrStopped = errors.New("async: TaskRunner is stopped")
)

// TaskRunner implements a concurrency-safe, unbounded worker pool using a generic Queue[func()].
type TaskRunner struct {
	queue   *Queue[func()]
	stopped bool
	mu      sync.Mutex
	wg      sync.WaitGroup
	active  int64
}

// NewTaskRunner creates and starts a new TaskRunner with the specified number of worker goroutines.
func NewTaskRunner(workers int) *TaskRunner {
	tr := &TaskRunner{
		queue: NewQueue[func()](),
	}
	tr.wg.Add(workers)
	for i := 0; i < workers; i++ {
		go tr.workerLoop()
	}
	return tr
}

func (tr *TaskRunner) workerLoop() {
	defer tr.wg.Done()
	for {
		task, ok := tr.queue.Pop()
		if !ok {
			return
		}
		atomic.AddInt64(&tr.active, 1)
		task()
		atomic.AddInt64(&tr.active, -1)
	}
}

// Submit enqueues a task for execution. Returns ErrStopped if the runner has been stopped.
func (tr *TaskRunner) Submit(task func()) error {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	if tr.stopped {
		return ErrStopped
	}
	tr.queue.Push(task)
	return nil
}

// TaskCount returns the total number of tasks currently in the queue or actively being processed.
func (tr *TaskRunner) TaskCount() int {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	qLen := tr.queue.Len()
	active := atomic.LoadInt64(&tr.active)
	return qLen + int(active)
}

// Close stops the runner, processes any remaining tasks, and waits for all workers to finish.
func (tr *TaskRunner) Close() error {
	tr.mu.Lock()
	if tr.stopped {
		tr.mu.Unlock()
		return nil
	}
	tr.stopped = true
	tr.mu.Unlock()

	tr.queue.Close()
	tr.wg.Wait()
	return nil
}

// Ensure interface compliance
var _ io.Closer = (*TaskRunner)(nil)
