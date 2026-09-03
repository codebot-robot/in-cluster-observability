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

package store

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"gocloud.dev/blob"
	_ "gocloud.dev/blob/fileblob"
	_ "gocloud.dev/blob/gcsblob"
	_ "gocloud.dev/blob/s3blob"
)

// ArchiveStore defines the interface for backend object archiving.
type ArchiveStore interface {
	// Upload uploads the local file at localPath to the archive store under key
	// only if it does not already exist with the same size.
	Upload(ctx context.Context, key string, localPath string) error

	// IsUploaded returns true if the file exists in the archive store under key
	// with the same size as the local file at localPath.
	IsUploaded(ctx context.Context, key string, localPath string) (bool, error)

	// Close closes any underlying connections or resources.
	io.Closer
}

// ArchiveBucket wraps *blob.Bucket with local metadata caching and prefix prepending.
type ArchiveBucket struct {
	bucket *blob.Bucket
	prefix string
	mu     sync.RWMutex
	cache  map[string]int64
}

// NewArchiveStore parses the archive URL, initializes the appropriate blob bucket,
// and returns an ArchiveStore. Supports gs://, s3://, and file:// URLs.
func NewArchiveStore(ctx context.Context, archiveURL string) (ArchiveStore, error) {
	if archiveURL == "" {
		return nil, nil
	}
	u, err := url.Parse(archiveURL)
	if err != nil {
		return nil, err
	}
	var bucketURL string
	if u.Scheme == "file" {
		absPath, err := filepath.Abs(u.Path)
		if err != nil {
			return nil, err
		}
		bucketURL = "file://" + absPath
		// Pre-create directory for fileblob if needed
		if err := os.MkdirAll(absPath, 0755); err != nil {
			return nil, fmt.Errorf("failed to create local bucket directory %s: %w", absPath, err)
		}
	} else {
		bucketURL = u.Scheme + "://" + u.Host
	}

	b, err := blob.OpenBucket(ctx, bucketURL)
	if err != nil {
		return nil, fmt.Errorf("failed to open bucket %s: %w", bucketURL, err)
	}

	var prefix string
	if u.Scheme != "file" {
		prefix = strings.Trim(u.Path, "/")
	}

	return &ArchiveBucket{
		bucket: b,
		prefix: prefix,
		cache:  make(map[string]int64),
	}, nil
}

func (a *ArchiveBucket) resolveKey(key string) string {
	if a.prefix == "" {
		return key
	}
	return a.prefix + "/" + key
}

// Upload uploads the local file at localPath to key if it does not already exist with the same size.
func (a *ArchiveBucket) Upload(ctx context.Context, key string, localPath string) error {
	info, err := os.Stat(localPath)
	if err != nil {
		return fmt.Errorf("failed to stat local file %s: %w", localPath, err)
	}
	localSize := info.Size()

	resolvedKey := a.resolveKey(key)

	// Check if already uploaded with same size
	uploaded, err := a.checkUploaded(ctx, resolvedKey, localSize)
	if err == nil && uploaded {
		return nil
	}

	data, err := os.ReadFile(localPath)
	if err != nil {
		return fmt.Errorf("failed to read local file %s: %w", localPath, err)
	}

	if err := a.bucket.WriteAll(ctx, resolvedKey, data, nil); err != nil {
		return fmt.Errorf("failed to upload key %s to bucket: %w", resolvedKey, err)
	}

	a.mu.Lock()
	a.cache[resolvedKey] = localSize
	a.mu.Unlock()

	return nil
}

// IsUploaded returns true if the file exists in the archive with the same size as the local file.
func (a *ArchiveBucket) IsUploaded(ctx context.Context, key string, localPath string) (bool, error) {
	info, err := os.Stat(localPath)
	if err != nil {
		return false, fmt.Errorf("failed to stat local file %s: %w", localPath, err)
	}
	return a.checkUploaded(ctx, a.resolveKey(key), info.Size())
}

func (a *ArchiveBucket) checkUploaded(ctx context.Context, resolvedKey string, expectedSize int64) (bool, error) {
	a.mu.RLock()
	cachedSize, exists := a.cache[resolvedKey]
	a.mu.RUnlock()
	if exists {
		return cachedSize == expectedSize, nil
	}

	attrs, err := a.bucket.Attributes(ctx, resolvedKey)
	if err != nil {
		return false, err
	}

	a.mu.Lock()
	a.cache[resolvedKey] = attrs.Size
	a.mu.Unlock()

	return attrs.Size == expectedSize, nil
}

// Close closes the underlying bucket connection.
func (a *ArchiveBucket) Close() error {
	return a.bucket.Close()
}

// Ensure interface compliance
var _ ArchiveStore = (*ArchiveBucket)(nil)
