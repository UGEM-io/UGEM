// Copyright (c) 2026 UGEM Community
// Licensed under the GNU Affero General Public License v3.0.
// See LICENSE_AGPL.md in the project root for full license information.
package storage

import (
	"bufio"
	"bytes"
	"encoding/gob"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var (
	ErrCorruptedWAL     = fmt.Errorf("corrupted write-ahead log")
	ErrSnapshotNotFound = fmt.Errorf("snapshot not found")
)

type TypedValue struct {
	Type  string
	Value interface{}
}

type Path string

type EventID uint64

type LogicalClock uint64

type TraceContext struct {
	TraceID       string
	ParentEventID string
	GoalID        string
	ActionID      string
}

type Event struct {
	ID           EventID
	Clock        LogicalClock
	Timestamp    time.Time
	Type         string
	ReadPaths    []Path
	WritePaths   []Path
	Payload      map[string]interface{}
	Trace        TraceContext
	Mutations    []Mutation
}

type Mutation struct {
	Path  Path
	Value TypedValue
}

type WALEntry struct {
	EventID   EventID
	Clock     LogicalClock
	Timestamp time.Time
	Event     Event
}

type WriteAheadLog struct {
	mu         sync.RWMutex
	file       *os.File
	path       string
	eventCount EventID
	clock      LogicalClock
}

func NewWriteAheadLog(path string) (*WriteAheadLog, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open WAL: %w", err)
	}

	wal := &WriteAheadLog{
		file: f,
		path: path,
	}

	wal.recoverPosition()

	return wal, nil
}

func (w *WriteAheadLog) recoverPosition() {
	events, err := w.replayUnlocked()
	if err != nil || len(events) == 0 {
		return
	}

	last := events[len(events)-1]
	w.eventCount = last.ID
	w.clock = last.Clock
}

func (w *WriteAheadLog) Append(event Event) (EventID, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.eventCount++
	event.ID = w.eventCount
	event.Clock = w.clock + 1
	event.Timestamp = time.Now()

	entry := WALEntry{
		EventID:   event.ID,
		Clock:     event.Clock,
		Timestamp: event.Timestamp,
		Event:     event,
	}

	data, err := json.Marshal(entry)
	if err != nil {
		return 0, fmt.Errorf("failed to marshal WAL entry: %w", err)
	}

	if _, err := w.file.Write(append(data, '\n')); err != nil {
		return 0, fmt.Errorf("failed to write WAL entry: %w", err)
	}

	if err := w.file.Sync(); err != nil {
		return 0, fmt.Errorf("failed to sync WAL: %w", err)
	}

	w.clock = event.Clock

	return event.ID, nil
}

func (w *WriteAheadLog) Replay() ([]Event, error) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.replayUnlocked()
}

// replayUnlocked reads all events from the WAL file without acquiring the lock.
// Caller must hold at least a read lock.
func (w *WriteAheadLog) replayUnlocked() ([]Event, error) {
	if _, err := w.file.Seek(0, os.SEEK_SET); err != nil {
		return nil, err
	}

	events := make([]Event, 0)
	scanner := bufio.NewScanner(w.file)
	// Use a larger buffer for potentially large events
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 10*1024*1024) // 10MB max event size

	for scanner.Scan() {
		var entry WALEntry
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			continue // Skip corrupted lines
		}
		events = append(events, entry.Event)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("WAL scan error: %w", err)
	}

	return events, nil
}

func (w *WriteAheadLog) Truncate(keepCount int) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	events, err := w.replayUnlocked()
	if err != nil {
		return err
	}

	if len(events) <= keepCount {
		return nil
	}

	eventsToKeep := events[len(events)-keepCount:]

	w.file.Close()

	tmpPath := w.path + ".tmp"
	tmp, err := os.Create(tmpPath)
	if err != nil {
		return err
	}
	defer tmp.Close()

	for _, event := range eventsToKeep {
		entry := WALEntry{
			EventID:   event.ID,
			Clock:     event.Clock,
			Timestamp: event.Timestamp,
			Event:     event,
		}
		data, err := json.Marshal(entry)
		if err != nil {
			return err
		}
		if _, err := tmp.Write(append(data, '\n')); err != nil {
			return err
		}
	}

	tmp.Sync()
	tmp.Close()

	if err := os.Rename(tmpPath, w.path); err != nil {
		return err
	}

	w.file, err = os.OpenFile(w.path, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0644)
	if err != nil {
		return err
	}

	w.eventCount = EventID(len(eventsToKeep))

	return nil
}

func (w *WriteAheadLog) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.file.Close()
}

func (w *WriteAheadLog) Len() int {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return int(w.eventCount)
}

func (w *WriteAheadLog) GetClock() LogicalClock {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.clock
}

type Snapshot struct {
	ID        uint64
	Clock     LogicalClock
	Timestamp time.Time
	Data      map[Path]TypedValue
}

type SnapshotStore struct {
	mu        sync.RWMutex
	snapshots map[uint64]*Snapshot
	path      string
	nextID    uint64
}

func NewSnapshotStore(path string) (*SnapshotStore, error) {
	if err := os.MkdirAll(path, 0755); err != nil {
		return nil, err
	}

	ss := &SnapshotStore{
		snapshots: make(map[uint64]*Snapshot),
		path:      path,
	}

	ss.recoverSnapshots()

	return ss, nil
}

func (ss *SnapshotStore) recoverSnapshots() {
	files, err := filepath.Glob(filepath.Join(ss.path, "snapshot_*.bin"))
	if err != nil {
		return
	}

	for _, f := range files {
		var snap Snapshot
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		if err := gob.NewDecoder(bytes.NewReader(data)).Decode(&snap); err != nil {
			continue
		}
		ss.snapshots[snap.ID] = &snap
		if snap.ID >= ss.nextID {
			ss.nextID = snap.ID + 1
		}
	}
}

func (ss *SnapshotStore) Save(state map[Path]TypedValue, clock LogicalClock) (uint64, error) {
	ss.mu.Lock()
	defer ss.mu.Unlock()

	snapshot := &Snapshot{
		ID:        ss.nextID,
		Clock:     clock,
		Timestamp: time.Now(),
		Data:      state,
	}

	filename := filepath.Join(ss.path, fmt.Sprintf("snapshot_%d.bin", ss.nextID))
	file, err := os.Create(filename)
	if err != nil {
		return 0, err
	}
	defer file.Close()

	if err := gob.NewEncoder(file).Encode(snapshot); err != nil {
		os.Remove(filename)
		return 0, err
	}

	file.Sync()

	ss.snapshots[ss.nextID] = snapshot
	ss.nextID++

	return ss.nextID - 1, nil
}

func (ss *SnapshotStore) Load(id uint64) (*Snapshot, error) {
	ss.mu.RLock()
	defer ss.mu.RUnlock()

	snap, exists := ss.snapshots[id]
	if !exists {
		return nil, ErrSnapshotNotFound
	}

	return snap, nil
}

func (ss *SnapshotStore) Latest() (*Snapshot, error) {
	ss.mu.RLock()
	defer ss.mu.RUnlock()

	if len(ss.snapshots) == 0 {
		return nil, ErrSnapshotNotFound
	}

	var latest *Snapshot
	for _, snap := range ss.snapshots {
		if latest == nil || snap.ID > latest.ID {
			latest = snap
		}
	}

	return latest, nil
}

func (ss *SnapshotStore) Delete(id uint64) error {
	ss.mu.Lock()
	defer ss.mu.Unlock()

	filename := filepath.Join(ss.path, fmt.Sprintf("snapshot_%d.bin", id))
	if err := os.Remove(filename); err != nil {
		return err
	}

	delete(ss.snapshots, id)
	return nil
}

func (ss *SnapshotStore) List() []*Snapshot {
	ss.mu.RLock()
	defer ss.mu.RUnlock()

	snapshots := make([]*Snapshot, 0, len(ss.snapshots))
	for _, snap := range ss.snapshots {
		snapshots = append(snapshots, snap)
	}

	return snapshots
}

type PersistentStore struct {
	WAL       *WriteAheadLog
	Snapshots *SnapshotStore
	mu        sync.RWMutex
	closed    bool
	dataDir   string
}

func NewPersistentStore(dataDir string) (*PersistentStore, error) {
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, err
	}

	wal, err := NewWriteAheadLog(filepath.Join(dataDir, "wal.log"))
	if err != nil {
		return nil, err
	}

	snapshots, err := NewSnapshotStore(filepath.Join(dataDir, "snapshots"))
	if err != nil {
		wal.Close()
		return nil, err
	}

	return &PersistentStore{
		WAL:       wal,
		Snapshots: snapshots,
		dataDir:   dataDir,
	}, nil
}

// SetObject stores a typed object and updates its secondary indexes.
func (ps *PersistentStore) SetObject(typeName string, id string, data map[string]interface{}) error {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	// Primary key: /type/<TypeName>/<ID>
	key := fmt.Sprintf("/type/%s/%s", typeName, id)
	path := filepath.Join(ps.dataDir, "objects", key+".bin")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}

	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()

	if err := gob.NewEncoder(file).Encode(data); err != nil {
		return err
	}

	// Update secondary indexes: /index/<TypeName>/<Field>/<Value>/<ID>
	for field, value := range data {
		indexKey := fmt.Sprintf("/index/%s/%s/%v/%s", typeName, field, value, id)
		indexPath := filepath.Join(ps.dataDir, "indexes", indexKey+".ptr")
		if err := os.MkdirAll(filepath.Dir(indexPath), 0755); err != nil {
			continue
		}
		os.WriteFile(indexPath, []byte(key), 0644)
	}

	return nil
}

// Search retrieves object IDs matching a type and field value.
func (ps *PersistentStore) Search(typeName string, field string, value interface{}) ([]string, error) {
	ps.mu.RLock()
	defer ps.mu.RUnlock()

	pattern := filepath.Join(ps.dataDir, "indexes", "index", typeName, field, fmt.Sprintf("%v", value), "*.ptr")
	files, err := filepath.Glob(pattern)
	if err != nil {
		return nil, err
	}

	ids := make([]string, 0, len(files))
	for _, f := range files {
		ids = append(ids, filepath.Base(strings.TrimSuffix(f, ".ptr")))
	}

	return ids, nil
}

func (ps *PersistentStore) Close() error {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	if ps.closed {
		return nil
	}

	ps.closed = true
	return ps.WAL.Close()
}

func (ps *PersistentStore) GetDataDir() string {
	return ps.dataDir
}
