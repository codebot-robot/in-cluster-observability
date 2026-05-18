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

package obiconfig

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Writer writes OBI config files atomically. Atomicity matters because
// OBI watches its config and reloads on change — we never want it to
// read a half-written file.
//
// Construct one Writer per agent process; it is safe for concurrent
// Write calls.
type Writer struct {
	path string
}

// NewWriter constructs a Writer that targets path. The parent
// directory must exist.
func NewWriter(path string) (*Writer, error) {
	if path == "" {
		return nil, fmt.Errorf("obiconfig: writer path must not be empty")
	}
	dir := filepath.Dir(path)
	if info, err := os.Stat(dir); err != nil {
		return nil, fmt.Errorf("obiconfig: parent dir %q: %w", dir, err)
	} else if !info.IsDir() {
		return nil, fmt.Errorf("obiconfig: parent %q is not a directory", dir)
	}
	return &Writer{path: path}, nil
}

// Path returns the file path this Writer targets.
func (w *Writer) Path() string { return w.path }

// Write marshals f to YAML and writes it atomically to the configured
// path (write-temp-then-rename). Returns true if the file content
// actually changed; false if the new content equals the existing
// content (caller can skip reload signaling in the unchanged case).
func (w *Writer) Write(f File) (changed bool, err error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(f); err != nil {
		return false, fmt.Errorf("obiconfig: marshal: %w", err)
	}
	if err := enc.Close(); err != nil {
		return false, fmt.Errorf("obiconfig: encoder close: %w", err)
	}

	newContent := buf.Bytes()

	// Short-circuit if unchanged.
	if existing, err := os.ReadFile(w.path); err == nil {
		if bytes.Equal(existing, newContent) {
			return false, nil
		}
	}

	tmp, err := os.CreateTemp(filepath.Dir(w.path), ".obi-config-*.yaml.tmp")
	if err != nil {
		return false, fmt.Errorf("obiconfig: create temp: %w", err)
	}
	tmpPath := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpPath) }

	if _, err := tmp.Write(newContent); err != nil {
		_ = tmp.Close()
		cleanup()
		return false, fmt.Errorf("obiconfig: write temp: %w", err)
	}
	// os.CreateTemp gives 0600 — owner-readable only. Our writer (the
	// ollie agent) and OBI's reader run in separate containers under
	// different uids; OBI must be able to read the file regardless of
	// the upstream image's runAsUser. World-readable is fine: the
	// config is non-sensitive and the file lives on an emptyDir
	// shared only between the two containers in this pod.
	if err := tmp.Chmod(0o644); err != nil {
		_ = tmp.Close()
		cleanup()
		return false, fmt.Errorf("obiconfig: chmod temp: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		cleanup()
		return false, fmt.Errorf("obiconfig: fsync temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return false, fmt.Errorf("obiconfig: close temp: %w", err)
	}
	if err := os.Rename(tmpPath, w.path); err != nil {
		cleanup()
		return false, fmt.Errorf("obiconfig: rename: %w", err)
	}
	return true, nil
}
