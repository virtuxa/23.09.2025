package storage

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"taskservice/internal/model"
)

// Реализует долговечное хранилище с использованием WAL + snapshot в одной директории
type Store struct {
	dir       string
	mu        sync.RWMutex
	tasks     map[model.TaskID]*model.Task
	walFile   *os.File
	walWriter *bufio.Writer
}

// Представляет запись в WAL
type walRecord struct {
	Type string      `json:"type"`
	Data interface{} `json:"data"`
}

// Представляет запись в WAL для upsert задачи
type recordUpsertTask struct {
	Task *model.Task `json:"task"`
}

// Представляет запись в WAL для update задачи
type recordUpdateTask struct {
	TaskID model.TaskID `json:"task_id"`
	Task   *model.Task  `json:"task"`
}

// Создает новый Store
func NewStore(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	s := &Store{dir: dir, tasks: make(map[model.TaskID]*model.Task)}
	if err := s.loadSnapshotAndWal(); err != nil {
		return nil, err
	}
	if err := s.openWal(); err != nil {
		return nil, err
	}
	return s, nil
}

// Закрывает Store
func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.walWriter != nil {
		s.walWriter.Flush()
	}
	if s.walFile != nil {
		return s.walFile.Close()
	}
	return nil
}

// Открывает WAL
func (s *Store) openWal() error {
	f, err := os.OpenFile(filepath.Join(s.dir, "state.wal"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	s.walFile = f
	s.walWriter = bufio.NewWriter(f)
	return nil
}

// Загружает snapshot и WAL
func (s *Store) loadSnapshotAndWal() error {
	// Загружает snapshot
	snapPath := filepath.Join(s.dir, "state.snapshot.json")
	if b, err := os.ReadFile(snapPath); err == nil {
		var snapshot map[model.TaskID]*model.Task
		if err := json.Unmarshal(b, &snapshot); err == nil {
			s.tasks = snapshot
		}
	}
	// Воспроизводит WAL
	walPath := filepath.Join(s.dir, "state.wal")
	if f, err := os.OpenFile(walPath, os.O_RDONLY|os.O_CREATE, 0o644); err == nil {
		defer f.Close()
		dec := json.NewDecoder(f)
		for {
			var rec walRecord
			if err := dec.Decode(&rec); err != nil {
				if errors.Is(err, os.ErrClosed) {
					break
				}
				break
			}
			switch rec.Type {
			case "upsert_task":
				raw, _ := json.Marshal(rec.Data)
				var r recordUpsertTask
				if err := json.Unmarshal(raw, &r); err == nil {
					s.tasks[r.Task.ID] = r.Task
				}
			case "update_task":
				raw, _ := json.Marshal(rec.Data)
				var r recordUpdateTask
				if err := json.Unmarshal(raw, &r); err == nil {
					s.tasks[r.TaskID] = r.Task
				}
			}
		}
	}
	return nil
}

// Добавляет запись в WAL
func (s *Store) appendRecord(rec walRecord) error {
	b, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	if _, err := s.walWriter.Write(append(b, '\n')); err != nil {
		return err
	}
	if err := s.walWriter.Flush(); err != nil {
		return err
	}
	return s.walFile.Sync()
}

// Сохраняет snapshot
func (s *Store) SaveSnapshot() error {
	s.mu.RLock()
	snapshot := make(map[model.TaskID]*model.Task, len(s.tasks))
	for k, v := range s.tasks {
		snapshot[k] = v
	}
	s.mu.RUnlock()

	b, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return err
	}
	tmp := filepath.Join(s.dir, "state.snapshot.json.tmp")
	final := filepath.Join(s.dir, "state.snapshot.json")
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, final); err != nil {
		return err
	}
	return nil
}

// UpsertTask создает или обновляет задачу
func (s *Store) UpsertTask(t *model.Task) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tasks[t.ID] = t
	return s.appendRecord(walRecord{Type: "upsert_task", Data: recordUpsertTask{Task: t}})
}

// UpdateTask обновляет задачу
func (s *Store) UpdateTask(t *model.Task) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tasks[t.ID] = t
	return s.appendRecord(walRecord{Type: "update_task", Data: recordUpdateTask{TaskID: t.ID, Task: t}})
}

// ListTasks получает все задачи
func (s *Store) ListTasks() []*model.Task {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*model.Task, 0, len(s.tasks))
	for _, t := range s.tasks {
		out = append(out, t)
	}
	return out
}

// GetTask получает задачу по id
func (s *Store) GetTask(id model.TaskID) (*model.Task, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, ok := s.tasks[id]
	return t, ok
}

// Debug helper
func (s *Store) Stats() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return fmt.Sprintf("tasks=%d", len(s.tasks))
}
