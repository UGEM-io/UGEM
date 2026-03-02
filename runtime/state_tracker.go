// Copyright (c) 2026 UGEM Community
// Licensed under the GNU Affero General Public License v3.0.
// See LICENSE_AGPL.md in the project root for full license information.
package runtime

import (
	"sync"
)

// StateTracker wraps an existing State to record which paths are accessed during evaluation.
// This allows dynamic dependency discovery for Goals.
type StateTracker struct {
	State
	mu        sync.RWMutex
	readPaths map[Path]bool
}

func NewStateTracker(base State) *StateTracker {
	return &StateTracker{
		State:     base,
		readPaths: make(map[Path]bool),
	}
}

func (s *StateTracker) Get(path Path) (TypedValue, bool) {
	s.mu.Lock()
	s.readPaths[path] = true
	s.mu.Unlock()

	return s.State.Get(path)
}

func (s *StateTracker) GetAccessedPaths() []Path {
	s.mu.RLock()
	defer s.mu.RUnlock()

	paths := make([]Path, 0, len(s.readPaths))
	for p := range s.readPaths {
		paths = append(paths, p)
	}
	return paths
}
