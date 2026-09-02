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

package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gke-labs/in-cluster-observability/opentelemetry/pkg/compactor"
)

func main() {
	archiveURL := flag.String("archive-url", "", "Bucket URL convention (e.g. file:///tmp/bucket, gs://my-bucket/prefix, s3://my-bucket/prefix)")
	once := flag.Bool("once", false, "Run compaction once and exit")
	interval := flag.Duration("interval", 5*time.Minute, "Compaction loop interval (if not running with --once)")
	settleDelay := flag.Duration("settle-delay", 30*time.Minute, "Delay past the hour to wait before compacting (default: 30m)")
	rowGroupSize := flag.Int("row-group-size", 100000, "Target number of rows per row group (default: 100000)")
	fileSizeLimit := flag.Int64("file-size-limit", 128*1024*1024, "Target file size in bytes before splitting (default: 128MB)")
	maxShardsPerRun := flag.Int("max-shards-per-run", 500, "Max raw shards to process in a single run (default: 500)")
	tmpDir := flag.String("tmp-dir", "", "Local temporary directory for downloading shards and writing Parquet files")

	flag.Parse()

	if *archiveURL == "" {
		log.Fatalf("Error: --archive-url must be specified")
	}

	cfg := &compactor.Config{
		ArchiveURL:      *archiveURL,
		SettleDelay:     *settleDelay,
		RowGroupSize:    *rowGroupSize,
		FileSizeLimit:   *fileSizeLimit,
		MaxShardsPerRun: *maxShardsPerRun,
		TmpDir:          *tmpDir,
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if *once {
		log.Println("Starting one-off compaction run...")
		if err := compactor.Compact(ctx, cfg); err != nil {
			log.Fatalf("Compaction failed: %v", err)
		}
		log.Println("Compaction completed successfully.")
		return
	}

	log.Printf("Starting compactor interval loop. Running every %v...", *interval)
	ticker := time.NewTicker(*interval)
	defer ticker.Stop()

	// Run immediately on start
	log.Println("Running initial compaction...")
	if err := compactor.Compact(ctx, cfg); err != nil {
		log.Printf("Compaction failed: %v", err)
	}

	for {
		select {
		case <-ctx.Done():
			log.Println("Shutting down compactor...")
			return
		case <-ticker.C:
			log.Println("Starting periodic compaction run...")
			if err := compactor.Compact(ctx, cfg); err != nil {
				log.Printf("Compaction failed: %v", err)
			}
		}
	}
}
