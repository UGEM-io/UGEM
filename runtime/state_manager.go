// Copyright (c) 2026 UGEM Community
// Licensed under the GNU Affero General Public License v3.0.
// See LICENSE_AGPL.md in the project root for full license information.
package runtime

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/ugem-io/ugem/storage"
)

var (
	ErrPathLocked       = errors.New("path is locked by another owner")
	ErrInvalidPath      = errors.New("invalid path")
	ErrSnapshotNotFound = errors.New("snapshot not found")
)

type pathLock struct {
	mu    sync.Mutex
	owner string
	refs  int
	cond  *sync.Cond
}

type stateSnapshot struct {
	id    uint64
	clock LogicalClock
	state map[Path]TypedValue
	mu    sync.RWMutex
}

func (s *stateSnapshot) ID() uint64 {
	return s.id
}

func (s *stateSnapshot) Clock() LogicalClock {
	return s.clock
}

func (s *stateSnapshot) State() map[Path]TypedValue {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make(map[Path]TypedValue, len(s.state))
	for k, v := range s.state {
		result[k] = v
	}
	return result
}

func (s *stateSnapshot) Get(path Path) (TypedValue, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.state[path]
	return v, ok
}

func (s *stateSnapshot) Set(path Path, value TypedValue) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state[path] = value
	return nil
}

func (s *stateSnapshot) Lock(paths []Path, owner string) error {
	return nil
}

func (s *stateSnapshot) Unlock(paths []Path, owner string) error {
	return nil
}

func (s *stateSnapshot) Apply(snap StateSnapshot) error {
	return nil
}

func (s *stateSnapshot) Diff(before StateSnapshot) (map[Path]TypedValue, error) {
	return make(map[Path]TypedValue), nil
}

func (s *stateSnapshot) Snapshot() (StateSnapshot, error) {
	return s, nil
}

type StateManagerImpl struct {
	mu          sync.RWMutex
	data        map[Path]TypedValue
	locks       map[Path]*pathLock
	clock       LogicalClock
	snapshots   map[uint64]*stateSnapshot
	snapshotID  uint64
	lockTimeout time.Duration
	version     uint64
	invariants  []Invariant
	pss         *storage.PersistentStore
}

func NewStateManager() *StateManagerImpl {
	return &StateManagerImpl{
		data:        make(map[Path]TypedValue),
		locks:       make(map[Path]*pathLock),
		snapshots:   make(map[uint64]*stateSnapshot),
		lockTimeout: 5 * time.Second,
	}
}

func (s *StateManagerImpl) SetPersistence(pss *storage.PersistentStore) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pss = pss
}

func (s *StateManagerImpl) RegisterInvariant(inv Invariant) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.invariants = append(s.invariants, inv)
}

func (s *StateManagerImpl) checkInvariants(state State) error {
	for _, inv := range s.invariants {
		if err := inv.Predicate(state); err != nil {
			return fmt.Errorf("invariant '%s' breached: %w", inv.Name, err)
		}
	}
	return nil
}

func (s *StateManagerImpl) Get(path Path) (TypedValue, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.data[path]
	return v, ok
}

func (s *StateManagerImpl) Set(path Path, value TypedValue) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if path == "" {
		return ErrInvalidPath
	}
	s.data[path] = value
	s.version++
	return nil
}

func (s *StateManagerImpl) Lock(paths []Path, owner string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, path := range paths {
		if path == "" {
			return ErrInvalidPath
		}
	}

	for _, path := range paths {
		lock, exists := s.locks[path]
		if !exists {
			newLock := &pathLock{}
			newLock.cond = sync.NewCond(&newLock.mu)
			lock = newLock
			s.locks[path] = lock
		}

		lock.mu.Lock()
		if lock.owner != "" && lock.owner != owner {
			lock.mu.Unlock()
			return ErrPathLocked
		}

		lock.owner = owner
		lock.refs++
		lock.mu.Unlock()
	}

	return nil
}

func (s *StateManagerImpl) Unlock(paths []Path, owner string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, path := range paths {
		lock, exists := s.locks[path]
		if !exists {
			continue
		}

		lock.mu.Lock()
		if lock.owner != owner {
			lock.mu.Unlock()
			continue
		}

		lock.refs--
		if lock.refs == 0 {
			lock.owner = ""
			lock.cond.Broadcast()
		}
		lock.mu.Unlock()
	}

	return nil
}

func (s *StateManagerImpl) Snapshot() (StateSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	snapshot := &stateSnapshot{
		id:    s.snapshotID,
		clock: s.clock,
		state: make(map[Path]TypedValue, len(s.data)),
	}

	for k, v := range s.data {
		snapshot.state[k] = v
	}

	s.snapshots[snapshot.id] = snapshot
	s.snapshotID++

	return snapshot, nil
}

func (s *StateManagerImpl) Apply(snap StateSnapshot) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	snapshot, ok := snap.(*stateSnapshot)
	if !ok {
		return ErrSnapshotNotFound
	}

	snapshot.mu.RLock()
	defer snapshot.mu.RUnlock()

	s.data = make(map[Path]TypedValue, len(snapshot.state))
	for k, v := range snapshot.state {
		s.data[k] = v
	}

	s.clock = snapshot.clock
	s.version++

	return nil
}

func (s *StateManagerImpl) Diff(before StateSnapshot) (map[Path]TypedValue, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	beforeSnap, ok := before.(*stateSnapshot)
	if !ok {
		return nil, ErrSnapshotNotFound
	}

	beforeSnap.mu.RLock()
	beforeState := make(map[Path]TypedValue)
	for k, v := range beforeSnap.state {
		beforeState[k] = v
	}
	beforeSnap.mu.RUnlock()

	diff := make(map[Path]TypedValue)

	for k, v := range s.data {
		if bv, exists := beforeState[k]; !exists || bv != v {
			diff[k] = v
		}
	}

	for k, v := range beforeState {
		if _, exists := s.data[k]; !exists {
			diff[k] = v
		}
	}

	return diff, nil
}

func (s *StateManagerImpl) ApplyEvent(event Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, path := range event.WritePaths {
		if lock, exists := s.locks[path]; exists {
			lock.mu.Lock()
			if lock.owner != "" {
				lock.mu.Unlock()
				return ErrPathLocked
			}
			lock.mu.Unlock()
		}
	}

	if event.StateMutator != nil {
		stateCopy := &stateSnapshot{
			id:    s.snapshotID,
			clock: s.clock,
			state: make(map[Path]TypedValue),
		}
		for k, v := range s.data {
			stateCopy.state[k] = v
		}

		if err := event.StateMutator(stateCopy); err != nil {
			return err
		}

		if err := s.checkInvariants(stateCopy); err != nil {
			return err
		}

		for k, v := range stateCopy.state {
			s.data[k] = v
		}

		if s.pss != nil {
			s.persistChanges(stateCopy.state)
		}
	}

	for _, m := range event.StateMutations {
		s.data[m.Path] = m.Value
	}

	s.clock++
	s.version++

	return nil
}

func (s *StateManagerImpl) GetSnapshot() (StateSnapshot, error) {
	return s.Snapshot()
}

func (s *StateManagerImpl) Reconstruct(events []Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.data = make(map[Path]TypedValue)
	s.clock = 0
	s.version = 0

	for _, event := range events {
		// Apply declarative mutations first
		for _, m := range event.StateMutations {
			s.data[m.Path] = m.Value
		}

		// Apply legacy function-based mutator if present
		if event.StateMutator != nil {
			stateCopy := &stateSnapshot{
				id:    s.snapshotID,
				clock: s.clock,
				state: make(map[Path]TypedValue),
			}
			for k, v := range s.data {
				stateCopy.state[k] = v
			}
			
			if err := event.StateMutator(stateCopy); err == nil {
				for k, v := range stateCopy.state {
					s.data[k] = v
				}
			}
		}
		s.clock = event.Clock
	}

	s.version++
	return nil
}

func (s *StateManagerImpl) GetLockManager() LockManager {
	return s
}

func (s *StateManagerImpl) GetClock() LogicalClock {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.clock
}

func (s *StateManagerImpl) AdvanceClock() LogicalClock {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clock++
	return s.clock
}

func (s *StateManagerImpl) IsLocked(path Path) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if lock, exists := s.locks[path]; exists {
		lock.mu.Lock()
		defer lock.mu.Unlock()
		return lock.owner != ""
	}
	return false
}

func (s *StateManagerImpl) GetLockOwner(path Path) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if lock, exists := s.locks[path]; exists {
		lock.mu.Lock()
		defer lock.mu.Unlock()
		return lock.owner, lock.owner != ""
	}
	return "", false
}

func (s *StateManagerImpl) persistChanges(data map[Path]TypedValue) {
	// Group paths by Type.ID
	objects := make(map[string]map[string]interface{})

	for path, val := range data {
		parts := strings.Split(string(path), ".")
		if len(parts) < 2 {
			continue // Skip singletons for now or handle them separately
		}

		typeName := parts[0]
		var id string
		var field string

		if len(parts) == 2 {
			id = "singleton"
			field = parts[1]
		} else {
			id = parts[1]
			field = strings.Join(parts[2:], ".")
		}

		key := typeName + ":" + id
		if _, exists := objects[key]; !exists {
			objects[key] = make(map[string]interface{})
		}
		objects[key][field] = val.Value
	}

	for key, fields := range objects {
		parts := strings.Split(key, ":")
		typeName, id := parts[0], parts[1]
		s.pss.SetObject(typeName, id, fields)
	}
}
