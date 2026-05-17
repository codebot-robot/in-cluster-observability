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

// Command ollie is the default binary that ships with the project.
// In v0.1 it is a stub — it prints version information and exits.
// Real functionality (agent / controller / query roles) lands in later
// milestones, wired through the public API in pkg/obsapi.
package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
)

// version is overridden at build time via -ldflags "-X main.version=...".
var version = "v0.1.0-dev"

func main() {
	versionOnly := flag.Bool("version", false, "print version and exit")
	stayAlive := flag.Bool("stay-alive", false, "block on SIGINT/SIGTERM after printing version (for DaemonSet deployments)")
	flag.Parse()

	if *versionOnly {
		fmt.Println(version)
		return
	}

	fmt.Fprintf(os.Stderr, "ollie %s\n", version)
	fmt.Fprintln(os.Stderr, "v0.1 Foundation: stub binary, no functionality wired yet.")
	fmt.Fprintln(os.Stderr, "Pass --version to print only the version string.")

	if !*stayAlive {
		return
	}
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	fmt.Fprintln(os.Stderr, "blocking on SIGINT/SIGTERM (--stay-alive)")
	sig := <-ch
	fmt.Fprintf(os.Stderr, "received %s; exiting\n", sig)
}
