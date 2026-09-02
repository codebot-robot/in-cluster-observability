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
	"encoding/binary"
	"hash/crc32"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	coltracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/proto"

	"github.com/gke-labs/in-cluster-observability/opentelemetry/cmd/opentelemetry-sink/pb"
)

func TestWriter(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "otel-test-dir-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	writer, err := NewWriter(tmpDir, "", 0)
	if err != nil {
		t.Fatalf("failed to create writer: %v", err)
	}
	defer writer.Close()

	ctx := t.Context()
	req := &coltracepb.ExportTraceServiceRequest{}
	if err := writer.WriteObject(ctx, req); err != nil {
		t.Fatalf("failed to write object: %v", err)
	}

	// Read back and verify
	f, err := os.Open(writer.currentShard)
	if err != nil {
		t.Fatalf("failed to open output file: %v", err)
	}
	defer f.Close()

	// Verify the 16-byte file header
	fileHeader := make([]byte, 16)
	if _, err := io.ReadFull(f, fileHeader); err != nil {
		t.Fatalf("failed to read file header: %v", err)
	}
	magic := binary.BigEndian.Uint32(fileHeader[0:4])
	version := binary.BigEndian.Uint32(fileHeader[4:8])
	if magic != fileMagic {
		t.Errorf("expected file magic %x, got %x", fileMagic, magic)
	}
	if version != fileVersion {
		t.Errorf("expected file version %d, got %d", fileVersion, version)
	}

	// 1st record should be ObjectType for ObjectType
	header := make([]byte, 16)
	if _, err := io.ReadFull(f, header); err != nil {
		t.Fatalf("failed to read 1st header: %v", err)
	}
	typeCode := binary.BigEndian.Uint32(header[12:16])
	if typeCode != uint32(TypeCode_ObjectType) {
		t.Errorf("expected typeCode %d, got %d", TypeCode_ObjectType, typeCode)
	}
	length := binary.BigEndian.Uint32(header[0:4])
	f.Seek(int64(length), io.SeekCurrent)

	// 2nd record should be ObjectType for ExportTraceServiceRequest
	if _, err := io.ReadFull(f, header); err != nil {
		t.Fatalf("failed to read 2nd header: %v", err)
	}
	typeCode = binary.BigEndian.Uint32(header[12:16])
	if typeCode != uint32(TypeCode_ObjectType) {
		t.Errorf("expected typeCode %d (ObjectType), got %d", TypeCode_ObjectType, typeCode)
	}
	length = binary.BigEndian.Uint32(header[0:4])
	f.Seek(int64(length), io.SeekCurrent)

	// 3rd record should be ExportTraceServiceRequest
	if _, err := io.ReadFull(f, header); err != nil {
		t.Fatalf("failed to read 3rd header: %v", err)
	}
	length = binary.BigEndian.Uint32(header[0:4])
	typeCode = binary.BigEndian.Uint32(header[12:16])
	if typeCode != 32 {
		t.Errorf("expected typeCode 32, got %d", typeCode)
	}
}

func corruptRecordInFile(t *testing.T, filename string, recordIndex int) {
	data, err := os.ReadFile(filename)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}

	offset := 0
	if len(data) >= 16 && binary.BigEndian.Uint32(data[0:4]) == fileMagic {
		offset = 16
	}
	currentIdx := 0
	for offset < len(data) {
		if offset+16 > len(data) {
			break
		}
		length := int(binary.BigEndian.Uint32(data[offset : offset+4]))
		if currentIdx == recordIndex {
			// Corrupt a byte in the payload of this record
			payloadOffset := offset + 16
			if payloadOffset < len(data) {
				data[payloadOffset] ^= 0xFF // Flip bits
				err = os.WriteFile(filename, data, 0644)
				if err != nil {
					t.Fatalf("failed to write corrupted file: %v", err)
				}
				return
			}
		}
		offset += 16 + length
		currentIdx++
	}
	t.Fatalf("could not find record %d to corrupt", recordIndex)
}

func truncateFileAtRecordPayload(t *testing.T, filename string, recordIndex int, truncateBytes int) {
	data, err := os.ReadFile(filename)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}

	offset := 0
	if len(data) >= 16 && binary.BigEndian.Uint32(data[0:4]) == fileMagic {
		offset = 16
	}
	currentIdx := 0
	for offset < len(data) {
		if offset+16 > len(data) {
			break
		}
		length := int(binary.BigEndian.Uint32(data[offset : offset+4]))
		if currentIdx == recordIndex {
			// Truncate the file here (e.g. keep some part of payload, or just truncate mid-header / mid-payload)
			truncatedData := data[:offset+16+truncateBytes]
			err = os.WriteFile(filename, truncatedData, 0644)
			if err != nil {
				t.Fatalf("failed to write truncated file: %v", err)
			}
			return
		}
		offset += 16 + length
		currentIdx++
	}
	t.Fatalf("could not find record %d to truncate", recordIndex)
}

func getRecordPayloadLen(t *testing.T, filename string, recordIndex int) int {
	data, err := os.ReadFile(filename)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}

	offset := 0
	if len(data) >= 16 && binary.BigEndian.Uint32(data[0:4]) == fileMagic {
		offset = 16
	}
	currentIdx := 0
	for offset < len(data) {
		if offset+16 > len(data) {
			break
		}
		length := int(binary.BigEndian.Uint32(data[offset : offset+4]))
		if currentIdx == recordIndex {
			return length
		}
		offset += 16 + length
		currentIdx++
	}
	t.Fatalf("could not find record %d to get payload len", recordIndex)
	return 0
}

func TestWriter_CRCVerification(t *testing.T) {
	// Subtest 1: Corrupted payload byte
	t.Run("CorruptedPayloadByte", func(t *testing.T) {
		tmpDir, err := os.MkdirTemp("", "otel-test-crc-*")
		if err != nil {
			t.Fatalf("failed to create temp dir: %v", err)
		}
		defer os.RemoveAll(tmpDir)

		writer, err := NewWriter(tmpDir, "", 0)
		if err != nil {
			t.Fatalf("failed to create writer: %v", err)
		}

		ctx := t.Context()
		req1 := &coltracepb.ExportTraceServiceRequest{
			ResourceSpans: []*tracepb.ResourceSpans{{SchemaUrl: "url-1"}},
		}
		req2 := &coltracepb.ExportTraceServiceRequest{
			ResourceSpans: []*tracepb.ResourceSpans{{SchemaUrl: "url-2"}},
		}
		req3 := &coltracepb.ExportTraceServiceRequest{
			ResourceSpans: []*tracepb.ResourceSpans{{SchemaUrl: "url-3"}},
		}

		if err := writer.WriteObject(ctx, req1); err != nil {
			t.Fatalf("failed to write req1: %v", err)
		}
		if err := writer.WriteObject(ctx, req2); err != nil {
			t.Fatalf("failed to write req2: %v", err)
		}
		if err := writer.WriteObject(ctx, req3); err != nil {
			t.Fatalf("failed to write req3: %v", err)
		}

		filename := writer.currentShard
		writer.Close()

		// Read and verify the file records:
		// Index 0: ObjectType (for ObjectType)
		// Index 1: ObjectType (for ExportTraceServiceRequest)
		// Index 2: req1 payload
		// Index 3: req2 payload
		// Index 4: req3 payload
		// We corrupt req2 payload (Index 3)
		corruptRecordInFile(t, filename, 3)

		// Create a writer object manually to call Query on the directory
		qWriter := &Writer{dir: tmpDir}

		// Capture log output
		var logBuf bytes.Buffer
		oldOutput := log.Writer()
		log.SetOutput(&logBuf)
		defer log.SetOutput(oldOutput)

		results, err := qWriter.Query(ctx, "")
		if err != nil {
			t.Fatalf("Query failed: %v", err)
		}

		// We expect only req1 to be returned (since req2 is corrupt, we stop reading there, so req3 is never read)
		if len(results) != 1 {
			t.Errorf("expected exactly 1 result (req1), got %d: %v", len(results), results)
		} else {
			resp, ok := results[0].(*coltracepb.ExportTraceServiceRequest)
			if !ok {
				t.Errorf("expected ExportTraceServiceRequest, got %T", results[0])
			} else if len(resp.ResourceSpans) != 1 || resp.ResourceSpans[0].SchemaUrl != "url-1" {
				t.Errorf("expected req1 (SchemaUrl 'url-1'), got %+v", resp.ResourceSpans)
			}
		}

		// Verify that a warning was logged
		logStr := logBuf.String()
		if !strings.Contains(logStr, "warning: CRC32 mismatch") {
			t.Errorf("expected CRC32 mismatch warning in logs, got: %q", logStr)
		}
	})

	// Subtest 2: Truncated tail (during header)
	t.Run("TruncatedTailHeader", func(t *testing.T) {
		tmpDir, err := os.MkdirTemp("", "otel-test-trunc-h-*")
		if err != nil {
			t.Fatalf("failed to create temp dir: %v", err)
		}
		defer os.RemoveAll(tmpDir)

		writer, err := NewWriter(tmpDir, "", 0)
		if err != nil {
			t.Fatalf("failed to create writer: %v", err)
		}

		ctx := t.Context()
		req1 := &coltracepb.ExportTraceServiceRequest{
			ResourceSpans: []*tracepb.ResourceSpans{{SchemaUrl: "url-1"}},
		}
		req2 := &coltracepb.ExportTraceServiceRequest{
			ResourceSpans: []*tracepb.ResourceSpans{{SchemaUrl: "url-2"}},
		}

		if err := writer.WriteObject(ctx, req1); err != nil {
			t.Fatalf("failed to write req1: %v", err)
		}
		if err := writer.WriteObject(ctx, req2); err != nil {
			t.Fatalf("failed to write req2: %v", err)
		}

		filename := writer.currentShard
		writer.Close()

		// Truncate file so req2 header is cut short (e.g. keep only 5 bytes of req2's header)
		// req2 is Index 3
		truncateFileAtRecordPayload(t, filename, 3, -11) // 16 bytes header, keeping 5 bytes of it means offset + 16 - 11

		qWriter := &Writer{dir: tmpDir}

		var logBuf bytes.Buffer
		oldOutput := log.Writer()
		log.SetOutput(&logBuf)
		defer log.SetOutput(oldOutput)

		results, err := qWriter.Query(ctx, "")
		if err != nil {
			t.Fatalf("Query failed: %v", err)
		}

		// Only req1 should be returned
		if len(results) != 1 {
			t.Errorf("expected exactly 1 result (req1), got %d: %v", len(results), results)
		}

		// Verify no warning/error logged
		logStr := logBuf.String()
		if strings.Contains(logStr, "warning") || strings.Contains(logStr, "failed to read") {
			t.Errorf("expected no warnings or failed read logs for truncated tail, got: %q", logStr)
		}
	})

	// Subtest 3: Truncated tail (during payload)
	t.Run("TruncatedTailPayload", func(t *testing.T) {
		tmpDir, err := os.MkdirTemp("", "otel-test-trunc-p-*")
		if err != nil {
			t.Fatalf("failed to create temp dir: %v", err)
		}
		defer os.RemoveAll(tmpDir)

		writer, err := NewWriter(tmpDir, "", 0)
		if err != nil {
			t.Fatalf("failed to create writer: %v", err)
		}

		ctx := t.Context()
		req1 := &coltracepb.ExportTraceServiceRequest{
			ResourceSpans: []*tracepb.ResourceSpans{{SchemaUrl: "url-1"}},
		}
		req2 := &coltracepb.ExportTraceServiceRequest{
			ResourceSpans: []*tracepb.ResourceSpans{{SchemaUrl: "url-2"}},
		}

		if err := writer.WriteObject(ctx, req1); err != nil {
			t.Fatalf("failed to write req1: %v", err)
		}
		if err := writer.WriteObject(ctx, req2); err != nil {
			t.Fatalf("failed to write req2: %v", err)
		}

		filename := writer.currentShard
		writer.Close()

		// Truncate file so req2 payload is cut short (e.g. keep only 2 bytes of req2's payload)
		// req2 is Index 3
		truncateFileAtRecordPayload(t, filename, 3, -(getRecordPayloadLen(t, filename, 3) - 2))

		qWriter := &Writer{dir: tmpDir}

		var logBuf bytes.Buffer
		oldOutput := log.Writer()
		log.SetOutput(&logBuf)
		defer log.SetOutput(oldOutput)

		results, err := qWriter.Query(ctx, "")
		if err != nil {
			t.Fatalf("Query failed: %v", err)
		}

		// Only req1 should be returned
		if len(results) != 1 {
			t.Errorf("expected exactly 1 result (req1), got %d: %v", len(results), results)
		}

		// Verify no warning/error logged
		logStr := logBuf.String()
		if strings.Contains(logStr, "warning") || strings.Contains(logStr, "failed to read") {
			t.Errorf("expected no warnings or failed read logs for truncated tail, got: %q", logStr)
		}
	})
}

func TestWriter_Query_Versions(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "otel-test-query-versions-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// 1. Create a legacy version 0 shard file (no magic, no version header)
	// We'll write the TypeCode mapping first, then a Trace record.
	legacyShard := filepath.Join(tmpDir, "shard-00000000000000000000.bin")
	lf, err := os.OpenFile(legacyShard, os.O_CREATE|os.O_TRUNC|os.O_RDWR, 0644)
	if err != nil {
		t.Fatalf("failed to create legacy shard: %v", err)
	}

	// ObjectType for ObjectType
	objType := &pb.ObjectType{
		TypeCode: uint32(TypeCode_ObjectType),
		TypeName: "otlptracefile.ObjectType",
	}
	objTypeData := objType.Marshal()
	lf.Write(makeRecordHeader(objTypeData, TypeCode_ObjectType))
	lf.Write(objTypeData)

	// ObjectType for ExportTraceServiceRequest
	traceType := &pb.ObjectType{
		TypeCode: 32,
		TypeName: "opentelemetry.proto.collector.trace.v1.ExportTraceServiceRequest",
	}
	traceTypeData := traceType.Marshal()
	lf.Write(makeRecordHeader(traceTypeData, TypeCode_ObjectType))
	lf.Write(traceTypeData)

	// Record of type ExportTraceServiceRequest
	traceMsg := &coltracepb.ExportTraceServiceRequest{}
	traceData, err := proto.Marshal(traceMsg)
	if err != nil {
		t.Fatalf("failed to marshal trace msg: %v", err)
	}
	lf.Write(makeRecordHeader(traceData, 32))
	lf.Write(traceData)
	lf.Close()

	// 2. Create a future version 2 shard file (has magic, but version is 2)
	futureShard := filepath.Join(tmpDir, "shard-00000000000000000002.bin")
	ff, err := os.OpenFile(futureShard, os.O_CREATE|os.O_TRUNC|os.O_RDWR, 0644)
	if err != nil {
		t.Fatalf("failed to create future shard: %v", err)
	}
	// Write magic and unsupported version (2) with 16-byte header
	fileHeader := make([]byte, 16)
	binary.BigEndian.PutUint32(fileHeader[0:4], fileMagic)
	binary.BigEndian.PutUint32(fileHeader[4:8], 2)
	ff.Write(fileHeader)

	// Write ObjectType anyway (even though it should be skipped)
	ff.Write(makeRecordHeader(objTypeData, TypeCode_ObjectType))
	ff.Write(objTypeData)
	ff.Close()

	// 3. Create a modern version 1 shard file (using NewWriter)
	writer, err := NewWriter(tmpDir, "", 0)
	if err != nil {
		t.Fatalf("failed to create writer: %v", err)
	}
	defer writer.Close()

	ctx := t.Context()
	// Write one trace request using the modern writer
	req := &coltracepb.ExportTraceServiceRequest{}
	if err := writer.WriteObject(ctx, req); err != nil {
		t.Fatalf("failed to write object: %v", err)
	}

	// 4. Run Query and verify we get exactly 2 messages:
	// - 1 from legacy (version 0)
	// - 1 from modern (version 1)
	// (The future version 2 should be skipped entirely, thus not adding any extra trace requests)
	results, err := writer.Query(ctx, "")
	if err != nil {
		t.Fatalf("failed to query: %v", err)
	}

	// We wrote 1 trace to legacy, and 1 to modern.
	// Since future version is skipped, we expect exactly 2 ExportTraceServiceRequest results.
	if len(results) != 2 {
		t.Errorf("expected 2 results, got %d", len(results))
	}
}

func makeRecordHeader(data []byte, typeCode TypeCode) []byte {
	header := make([]byte, 16)
	binary.BigEndian.PutUint32(header[0:4], uint32(len(data)))
	binary.BigEndian.PutUint32(header[4:8], crc32.ChecksumIEEE(data))
	binary.BigEndian.PutUint32(header[8:12], 0) // Flags
	binary.BigEndian.PutUint32(header[12:16], uint32(typeCode))
	return header
}
