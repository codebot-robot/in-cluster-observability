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

package sink_test

import (
	"context"
	"testing"

	"github.com/gke-labs/in-cluster-observability/pkg/sink"
)

type fakeSink struct{ name string }

func (f *fakeSink) Name() string                            { return f.name }
func (f *fakeSink) Init(context.Context, sink.Deps) error   { return nil }
func (f *fakeSink) Start(context.Context) error             { return nil }
func (f *fakeSink) Stop(context.Context) error              { return nil }
func (f *fakeSink) Write(context.Context, sink.Batch) error { return nil }

func TestRegistry_EmptyByDefault(t *testing.T) {
	r := sink.NewRegistry()
	if got := r.List(); len(got) != 0 {
		t.Fatalf("empty registry; got %d sinks", len(got))
	}
}

func TestRegistry_RegisterUnregister(t *testing.T) {
	r := sink.NewRegistry()
	a := &fakeSink{name: "a"}
	b := &fakeSink{name: "b"}

	if err := r.Register(a); err != nil {
		t.Fatalf("register a: %v", err)
	}
	if err := r.Register(b); err != nil {
		t.Fatalf("register b: %v", err)
	}
	if got := r.List(); len(got) != 2 || got[0].Name() != "a" || got[1].Name() != "b" {
		t.Fatalf("expected sorted [a, b]; got %v", got)
	}

	if err := r.Register(a); err == nil {
		t.Fatalf("duplicate register should error")
	}
	if err := r.Unregister("a"); err != nil {
		t.Fatalf("unregister a: %v", err)
	}
	if err := r.Unregister("a"); err == nil {
		t.Fatalf("double unregister should error")
	}
	if got := r.List(); len(got) != 1 || got[0].Name() != "b" {
		t.Fatalf("expected [b]; got %v", got)
	}
}

func TestRegistry_RejectInvalidSinks(t *testing.T) {
	r := sink.NewRegistry()
	if err := r.Register(nil); err == nil {
		t.Fatalf("nil sink should be rejected")
	}
	if err := r.Register(&fakeSink{name: ""}); err == nil {
		t.Fatalf("empty-name sink should be rejected")
	}
}
