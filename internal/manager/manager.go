package manager

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"taskservice/internal/model"
	"taskservice/internal/storage"
	"taskservice/internal/util"
)

// Конфигурация менеджера
type Config struct {
	Store           *storage.Store
	DataDir         string
	WorkerCount     int
	MaxRetryPerItem int
	BaseBackoff     time.Duration
	SnapshotEveryN  int
}

// Менеджер
type Manager struct {
	cfg        Config
	store      *storage.Store
	tasksMu    sync.RWMutex
	taskLocks  map[model.TaskID]*sync.Mutex
	queue      chan queueItem
	wg         sync.WaitGroup
	stopOnce   sync.Once
	stopCh     chan struct{}
	processedN int
}

// Элемент очереди
type queueItem struct {
	taskID  model.TaskID
	itemIdx int
}

// Создает новый Manager
func NewManager(cfg Config) (*Manager, error) {
	if cfg.Store == nil {
		return nil, errors.New("store is required")
	}
	if cfg.WorkerCount <= 0 {
		cfg.WorkerCount = 4
	}
	m := &Manager{
		cfg:       cfg,
		store:     cfg.Store,
		taskLocks: make(map[model.TaskID]*sync.Mutex),
		queue:     make(chan queueItem, 1024),
		stopCh:    make(chan struct{}),
	}
	return m, nil
}

// Запускает Manager
func (m *Manager) Start() error {
	// Повторная очередь незавершенных элементов после перезапуска
	tasks := m.store.ListTasks()
	for _, t := range tasks {
		for idx := range t.Items {
			it := &t.Items[idx]
			if it.Status != model.ItemStatusDone {
				it.Status = model.ItemStatusQueued
				it.ErrorMessage = ""
				it.StartedAt = nil
				it.CompletedAt = nil
				_ = m.store.UpdateTask(t)
				m.queue <- queueItem{taskID: t.ID, itemIdx: idx}
			}
		}
		if t.Status != model.TaskStatusCompleted {
			t.Status = model.TaskStatusPending
			_ = m.store.UpdateTask(t)
		}
	}

	for i := 0; i < m.cfg.WorkerCount; i++ {
		m.wg.Add(1)
		go m.worker()
	}
	return nil
}

// Останавливает Manager и ждет завершения всех задач
func (m *Manager) StopAndWait(ctx context.Context) error {
	var err error
	m.stopOnce.Do(func() {
		close(m.stopCh)
		close(m.queue)
		done := make(chan struct{})
		go func() {
			m.wg.Wait()
			close(done)
		}()
		select {
		case <-ctx.Done():
			err = ctx.Err()
		case <-done:
		}
	})
	return err
}

// Публичный API, используемый HTTP-слоем
func (m *Manager) CreateTask(urls []string) (model.TaskID, error) {
	t := &model.Task{
		ID:        model.TaskID(util.NewID()),
		CreatedAt: time.Now(),
		Status:    model.TaskStatusPending,
	}
	t.Items = make([]model.Item, 0, len(urls))
	for _, u := range urls {
		t.Items = append(t.Items, model.Item{
			URL:      u,
			FileName: model.DeriveDeterministicFileName(u),
			Status:   model.ItemStatusQueued,
		})
	}
	if err := m.store.UpsertTask(t); err != nil {
		return "", err
	}
	for idx := range t.Items {
		m.queue <- queueItem{taskID: t.ID, itemIdx: idx}
	}
	return t.ID, nil
}

// Список задач
func (m *Manager) ListTasks() []*model.Task                    { return m.store.ListTasks() }
func (m *Manager) GetTask(id model.TaskID) (*model.Task, bool) { return m.store.GetTask(id) }

// Воркер
func (m *Manager) worker() {
	defer m.wg.Done()
	client := &http.Client{Timeout: 0}
	for item := range m.queue {
		select {
		case <-m.stopCh:
			return
		default:
		}
		// Сериализация обработки в рамках одной задачи, чтобы избежать одновременных записей в одну структуру Task
		lock := m.getTaskLock(item.taskID)
		lock.Lock()
		m.processQueueItem(client, item)
		lock.Unlock()
		m.processedN++
		if m.cfg.SnapshotEveryN > 0 && m.processedN%m.cfg.SnapshotEveryN == 0 {
			_ = m.store.SaveSnapshot()
		}
	}
}

// Получение блокировки для задачи
func (m *Manager) getTaskLock(id model.TaskID) *sync.Mutex {
	m.tasksMu.Lock()
	defer m.tasksMu.Unlock()
	if l, ok := m.taskLocks[id]; ok {
		return l
	}
	l := &sync.Mutex{}
	m.taskLocks[id] = l
	return l
}

// Обработка элемента очереди
func (m *Manager) processQueueItem(client *http.Client, qi queueItem) {
	t, ok := m.store.GetTask(qi.taskID)
	if !ok {
		return
	}
	if qi.itemIdx < 0 || qi.itemIdx >= len(t.Items) {
		return
	}
	it := &t.Items[qi.itemIdx]
	t.Status = model.TaskStatusRunning
	now := time.Now()
	it.StartedAt = &now
	it.Status = model.ItemStatusDownloading
	_ = m.store.UpdateTask(t)

	// Убеждаемся, что директории существуют
	if err := os.MkdirAll(m.cfg.DataDir, 0o755); err != nil {
		m.failItem(t, it, fmt.Errorf("mkdir: %w", err))
		return
	}
	dstPath := filepath.Join(m.cfg.DataDir, it.FileName)
	tmpPath := dstPath + ".part"

	// Поддержка возобновления, если сервер позволяет Range
	var startOffset int64
	if fi, err := os.Stat(tmpPath); err == nil {
		startOffset = fi.Size()
	}

	req, err := http.NewRequest("GET", it.URL, nil)
	if err != nil {
		m.retryOrFail(t, it, err)
		return
	}
	if startOffset > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", startOffset))
	}
	resp, err := client.Do(req)
	if err != nil {
		m.retryOrFail(t, it, err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		m.retryOrFail(t, it, fmt.Errorf("bad status: %s", resp.Status))
		return
	}

	// Ожидаемый размер, если предоставлен
	if resp.ContentLength > 0 {
		it.SizeExpected = startOffset + resp.ContentLength
	}

	// Открываем файл для добавления
	f, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		m.retryOrFail(t, it, err)
		return
	}
	if _, err := f.Seek(startOffset, 0); err != nil {
		f.Close()
		m.retryOrFail(t, it, err)
		return
	}

	written, err := io.Copy(f, resp.Body)
	if cerr := f.Close(); cerr != nil && err == nil {
		err = cerr
	}
	it.SizeDownloaded = startOffset + written
	if err != nil {
		m.retryOrFail(t, it, err)
		return
	}

	// Атомарная переименование в окончательное имя
	if err := os.Rename(tmpPath, dstPath); err != nil {
		m.retryOrFail(t, it, err)
		return
	}

	done := time.Now()
	it.CompletedAt = &done
	it.Status = model.ItemStatusDone
	it.ErrorMessage = ""
	_ = m.store.UpdateTask(t)

	// Если все элементы завершены -> задача завершена
	allDone := true
	for i := range t.Items {
		if t.Items[i].Status != model.ItemStatusDone {
			allDone = false
			break
		}
	}
	if allDone {
		t.Status = model.TaskStatusCompleted
		_ = m.store.UpdateTask(t)
	}
}

// Повторная попытка или сбой
func (m *Manager) retryOrFail(t *model.Task, it *model.Item, cause error) {
	it.Attempts++
	it.ErrorMessage = cause.Error()
	it.Status = model.ItemStatusError
	_ = m.store.UpdateTask(t)
	if it.Attempts <= m.cfg.MaxRetryPerItem {
		backoff := m.cfg.BaseBackoff * time.Duration(it.Attempts)
		time.AfterFunc(backoff, func() {
			it.Status = model.ItemStatusQueued
			_ = m.store.UpdateTask(t)
			m.queue <- queueItem{taskID: t.ID, itemIdx: indexOfItem(t, it)}
		})
		return
	}
	// Если любой элемент завершился с ошибкой после повторных попыток, отметить задачу как неудачную, когда нет запущенных или в очереди элементов
	anyPending := false
	for i := range t.Items {
		s := t.Items[i].Status
		if s == model.ItemStatusQueued || s == model.ItemStatusDownloading {
			anyPending = true
			break
		}
	}
	if !anyPending {
		t.Status = model.TaskStatusFailed
		_ = m.store.UpdateTask(t)
	}
}

// Сбой элемента
func (m *Manager) failItem(t *model.Task, it *model.Item, cause error) {
	it.Attempts++
	it.ErrorMessage = cause.Error()
	it.Status = model.ItemStatusError
	_ = m.store.UpdateTask(t)
}

// Индекс элемента
func indexOfItem(t *model.Task, it *model.Item) int {
	for i := range t.Items {
		if &t.Items[i] == it {
			return i
		}
	}
	return -1
}
