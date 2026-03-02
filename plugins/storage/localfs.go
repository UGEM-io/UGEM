// Copyright (c) 2026 UGEM Community
// Licensed under the MIT License.
// See LICENSE_MIT.md in the project root for full license information.
package storage

import (
	"context"
	"crypto/sha256"
	"fmt"
	"github.com/ugem-io/ugem/runtime"
	"os"
	"path/filepath"
)

type LocalFS struct {
	baseDir string
}

func NewLocalFS() *LocalFS {
	return &LocalFS{}
}

func (l *LocalFS) Name() string {
	return "localfs"
}

func (l *LocalFS) Init(ctx context.Context, config map[string]string) error {
	l.baseDir = config["base_dir"]
	if l.baseDir == "" {
		l.baseDir = "./storage/files"
	}
	return os.MkdirAll(l.baseDir, 0755)
}

func (l *LocalFS) Put(ctx context.Context, data []byte) (runtime.File, error) {
	hash := fmt.Sprintf("%x", sha256.Sum256(data))
	id := hash[:16] // Use prefix of hash as ID for simplicity
	uri := filepath.Join(l.baseDir, hash)

	if err := os.WriteFile(uri, data, 0644); err != nil {
		return runtime.File{}, err
	}

	return runtime.File{
		ID:   id,
		URI:  uri,
		Hash: hash,
		Size: int64(len(data)),
		Mime: "application/octet-stream", // Basic for now
	}, nil
}

func (l *LocalFS) Get(ctx context.Context, uri string) ([]byte, error) {
	return os.ReadFile(uri)
}

func (l *LocalFS) Delete(ctx context.Context, uri string) error {
	return os.Remove(uri)
}

func (l *LocalFS) Exists(ctx context.Context, uri string) bool {
	_, err := os.Stat(uri)
	return err == nil
}

func (l *LocalFS) Actions() map[string]runtime.ActionHandler {
	return map[string]runtime.ActionHandler{
		"file.upload": func(input map[string]interface{}, ctx runtime.ActionContext) (map[string]interface{}, error) {
			data, ok := input["data"].([]byte)
			if !ok {
				// Handle string data if provided
				if s, ok := input["data"].(string); ok {
					data = []byte(s)
				} else {
					return nil, fmt.Errorf("invalid file data")
				}
			}
			file, err := l.Put(ctx.CancelCtx, data)
			if err != nil {
				return nil, err
			}
			return map[string]interface{}{
				"id":   file.ID,
				"uri":  file.URI,
				"hash": file.Hash,
				"size": file.Size,
				"mime": file.Mime,
			}, nil
		},
		"file.delete": func(input map[string]interface{}, ctx runtime.ActionContext) (map[string]interface{}, error) {
			uri, _ := input["uri"].(string)
			if err := l.Delete(ctx.CancelCtx, uri); err != nil {
				return nil, err
			}
			return map[string]interface{}{"status": "deleted"}, nil
		},
	}
}
