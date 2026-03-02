// Copyright (c) 2026 UGEM Community
// Licensed under the MIT License.
// See LICENSE_MIT.md in the project root for full license information.
package plugins

import (
	"context"
	"fmt"
	"github.com/ugem-io/ugem/runtime"
	"sync"
)

type Registry struct {
	mu      sync.RWMutex
	plugins map[string]runtime.Plugin
}

func NewRegistry() *Registry {
	return &Registry{
		plugins: make(map[string]runtime.Plugin),
	}
}

func (r *Registry) Register(p runtime.Plugin) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.plugins[p.Name()] = p
}

func (r *Registry) Get(name string) (runtime.Plugin, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.plugins[name]
	return p, ok
}

func (r *Registry) InitAll(ctx context.Context, configs map[string]map[string]string) error {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for name, p := range r.plugins {
		config := configs[name]
		if err := p.Init(ctx, config); err != nil {
			return fmt.Errorf("plugin %s init failed: %w", name, err)
		}
	}
	return nil
}
