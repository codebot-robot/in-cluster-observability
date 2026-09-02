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
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestTaskRunner(t *testing.T) {
	tr := NewTaskRunner(3)

	var count int64
	var wg sync.WaitGroup
	wg.Add(10)

	for i := 0; i < 10; i++ {
		err := tr.Submit(func() {
			atomic.AddInt64(&count, 1)
			wg.Done()
		})
		if err != nil {
			t.Errorf("unexpected error submitting task: %v", err)
		}
	}

	wg.Wait()
	if atomic.LoadInt64(&count) != 10 {
		t.Errorf("expected 10 tasks to run, got %d", count)
	}

	if err := tr.Close(); err != nil {
		t.Errorf("unexpected error closing TaskRunner: %v", err)
	}

	// Submit should return error after stopped
	err := tr.Submit(func() {})
	if err != ErrStopped {
		t.Errorf("expected ErrStopped, got %v", err)
	}
}

func TestTaskRunner_DrainsRemainingTasksOnClose(t *testing.T) {
	tr := NewTaskRunner(1)

	var count int64
	// Enqueue tasks, the first one blocks for a bit
	err := tr.Submit(func() {
		time.Sleep(50 * time.Millisecond)
		atomic.AddInt64(&count, 1)
	})
	if err != nil {
		t.Fatalf("failed to submit: %v", err)
	}

	for i := 0; i < 5; i++ {
		err = tr.Submit(func() {
			atomic.AddInt64(&count, 1)
		})
		if err != nil {
			t.Fatalf("failed to submit: %v", err)
		}
	}

	// Close immediately while first is running and others are queued
	if err := tr.Close(); err != nil {
		t.Errorf("failed to close: %v", err)
	}

	if atomic.LoadInt64(&count) != 6 {
		t.Errorf("expected all 6 tasks to finish on close draining, got %d", count)
	}
}
