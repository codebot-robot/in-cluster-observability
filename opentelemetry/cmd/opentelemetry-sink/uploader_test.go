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
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	coltracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
)

func TestUploader_DisabledMode(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "otel-uploader-disabled-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// NewWriter with empty archive URL
	writer, err := NewWriter(tmpDir, "", 0)
	if err != nil {
		t.Fatalf("failed to create writer: %v", err)
	}
	defer writer.Close()

	if writer.archiveStore != nil {
		t.Error("expected archiveStore to be nil when archiveURL is empty")
	}
	if writer.uploader != nil {
		t.Error("expected uploader to be nil when archiveURL is empty")
	}

	ctx := t.Context()
	req := &coltracepb.ExportTraceServiceRequest{}
	if err := writer.WriteObject(ctx, req); err != nil {
		t.Fatalf("failed to write object: %v", err)
	}

	// Rotate shard, verify no uploads are queued
	if err := writer.rotateShard(); err != nil {
		t.Fatalf("failed to rotate shard: %v", err)
	}

	if writer.uploader != nil && writer.uploader.TaskCount() != 0 {
		t.Errorf("expected 0 pending uploads, got %d", writer.uploader.TaskCount())
	}
}

func TestUploader_UploadOnRotate(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "otel-uploader-rotate-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	archiveDir, err := os.MkdirTemp("", "otel-archive-*")
	if err != nil {
		t.Fatalf("failed to create archive dir: %v", err)
	}
	defer os.RemoveAll(archiveDir)

	archiveURL := "file://" + archiveDir
	writer, err := NewWriter(tmpDir, archiveURL, 1*time.Hour)
	if err != nil {
		t.Fatalf("failed to create writer with archive: %v", err)
	}
	defer writer.Close()

	ctx := t.Context()
	req := &coltracepb.ExportTraceServiceRequest{}
	if err := writer.WriteObject(ctx, req); err != nil {
		t.Fatalf("failed to write object: %v", err)
	}

	// Rotate to close the first shard and trigger upload
	oldShard := writer.currentShard
	if err := writer.rotateShard(); err != nil {
		t.Fatalf("failed to rotate shard: %v", err)
	}

	// Poll for pendingUploads to become 0 and oldShard to be uploaded
	expectedKey := "raw/" + writer.podName + "/" + filepath.Base(oldShard)
	expectedDestFile := filepath.Join(archiveDir, expectedKey)

	success := false
	for i := 0; i < 50; i++ {
		if _, err := os.Stat(expectedDestFile); err == nil {
			success = true
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	if !success {
		t.Fatalf("timed out waiting for rotated shard to be uploaded to %s", expectedDestFile)
	}

	// Verify file sizes and content match
	localData, err := os.ReadFile(oldShard)
	if err != nil {
		t.Fatalf("failed to read local shard: %v", err)
	}
	remoteData, err := os.ReadFile(expectedDestFile)
	if err != nil {
		t.Fatalf("failed to read archived shard: %v", err)
	}

	if !bytes.Equal(localData, remoteData) {
		t.Error("archived shard content does not match local shard content")
	}
}

func TestUploader_StartupCatchUp(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "otel-uploader-catchup-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	archiveDir, err := os.MkdirTemp("", "otel-archive-catchup-*")
	if err != nil {
		t.Fatalf("failed to create archive dir: %v", err)
	}
	defer os.RemoveAll(archiveDir)

	// Pre-create two dummy shards in the local directory
	shard1Path := filepath.Join(tmpDir, "shard-00000000000000000001.bin")
	shard2Path := filepath.Join(tmpDir, "shard-00000000000000000002.bin")

	dummyContent1 := []byte("dummy shard 1 data")
	dummyContent2 := []byte("dummy shard 2 data")

	if err := os.WriteFile(shard1Path, dummyContent1, 0644); err != nil {
		t.Fatalf("failed to write dummy shard 1: %v", err)
	}
	if err := os.WriteFile(shard2Path, dummyContent2, 0644); err != nil {
		t.Fatalf("failed to write dummy shard 2: %v", err)
	}

	// Initialize NewWriter - this should immediately trigger crash catch-up
	archiveURL := "file://" + archiveDir
	writer, err := NewWriter(tmpDir, archiveURL, 1*time.Hour)
	if err != nil {
		t.Fatalf("failed to create writer: %v", err)
	}
	defer writer.Close()

	// Wait for uploads to complete
	expectedFile1 := filepath.Join(archiveDir, "raw/"+writer.podName+"/shard-00000000000000000001.bin")
	expectedFile2 := filepath.Join(archiveDir, "raw/"+writer.podName+"/shard-00000000000000000002.bin")

	success1, success2 := false, false
	for i := 0; i < 50; i++ {
		if _, err := os.Stat(expectedFile1); err == nil {
			success1 = true
		}
		if _, err := os.Stat(expectedFile2); err == nil {
			success2 = true
		}
		if success1 && success2 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	if !success1 {
		t.Errorf("failed to catch up and upload %s", expectedFile1)
	}
	if !success2 {
		t.Errorf("failed to catch up and upload %s", expectedFile2)
	}

	// Verify content
	if data, err := os.ReadFile(expectedFile1); err != nil || !bytes.Equal(data, dummyContent1) {
		t.Errorf("uploaded shard 1 content mismatch or read failed: %v", err)
	}
	if data, err := os.ReadFile(expectedFile2); err != nil || !bytes.Equal(data, dummyContent2) {
		t.Errorf("uploaded shard 2 content mismatch or read failed: %v", err)
	}
}

func TestUploader_LocalRetention(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "otel-uploader-retention-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	archiveDir, err := os.MkdirTemp("", "otel-archive-retention-*")
	if err != nil {
		t.Fatalf("failed to create archive dir: %v", err)
	}
	defer os.RemoveAll(archiveDir)

	// Set retention to a tiny value (1ms)
	archiveURL := "file://" + archiveDir
	writer, err := NewWriter(tmpDir, archiveURL, 1*time.Millisecond)
	if err != nil {
		t.Fatalf("failed to create writer: %v", err)
	}
	defer writer.Close()

	ctx := t.Context()
	req := &coltracepb.ExportTraceServiceRequest{}
	if err := writer.WriteObject(ctx, req); err != nil {
		t.Fatalf("failed to write object: %v", err)
	}

	oldShard := writer.currentShard

	// Wait 10ms to ensure the file's age is greater than retention (1ms)
	time.Sleep(10 * time.Millisecond)

	// Rotate shard, triggering upload of oldShard, followed by retention cleanup
	if err := writer.rotateShard(); err != nil {
		t.Fatalf("failed to rotate shard: %v", err)
	}

	// Wait for the upload of oldShard to finish and retention cleanup to remove it
	success := false
	for i := 0; i < 50; i++ {
		_, err := os.Stat(oldShard)
		if os.IsNotExist(err) {
			success = true
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	if !success {
		t.Errorf("local shard %s was not deleted after retention window expired", oldShard)
	}

	// Verify the file was still uploaded successfully to the archive
	expectedKey := "raw/" + writer.podName + "/" + filepath.Base(oldShard)
	expectedDestFile := filepath.Join(archiveDir, expectedKey)
	if _, err := os.Stat(expectedDestFile); err != nil {
		t.Errorf("archived file %s does not exist: %v", expectedDestFile, err)
	}

	// Verify that currentShard is NOT deleted
	if _, err := os.Stat(writer.currentShard); err != nil {
		t.Errorf("currentShard %s was unexpectedly deleted: %v", writer.currentShard, err)
	}
}
