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

// Package archtest hosts architecture-level invariants enforced as
// Go tests. The first invariant: only pkg/capture may import
// go.opentelemetry.io/obi/... — per ADR-0010 OBI's v0 churn is
// quarantined behind the capture adapter. This test fails if any
// other file in the module reaches OBI directly.
package archtest

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

const (
	moduleRoot   = "../.." // tests run with cwd = package dir
	allowedPath  = "pkg/capture"
	bannedPrefix = "go.opentelemetry.io/obi"
)

func TestNoOBIImportsOutsideCapture(t *testing.T) {
	root, err := filepath.Abs(moduleRoot)
	if err != nil {
		t.Fatalf("abs(%q): %v", moduleRoot, err)
	}
	fset := token.NewFileSet()
	var violations []string

	walker := func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			name := d.Name()
			if name == ".git" || name == "vendor" || name == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		// Allow capture itself to import OBI.
		if strings.HasPrefix(filepath.ToSlash(rel), allowedPath+"/") {
			return nil
		}
		file, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			return nil // best-effort; ignore unparseable files
		}
		for _, imp := range file.Imports {
			p := strings.Trim(imp.Path.Value, `"`)
			if strings.HasPrefix(p, bannedPrefix) {
				violations = append(violations, rel+" -> "+p)
			}
		}
		return nil
	}

	if err := filepath.WalkDir(root, walker); err != nil {
		t.Fatalf("walk: %v", err)
	}

	if len(violations) > 0 {
		t.Errorf("ADR-0010 violation: only pkg/capture may import %s/...", bannedPrefix)
		for _, v := range violations {
			t.Errorf("  %s", v)
		}
	}
}
