package model

import (
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"path"
	"strings"
	"time"
)

// TaskID представляет идентификатор задачи
type TaskID string

// TaskStatus представляет статус задачи
type TaskStatus string

const (
	// TaskStatusPending представляет статус задачи в ожидании
	TaskStatusPending TaskStatus = "pending"
	TaskStatusRunning TaskStatus = "running"
	// TaskStatusCompleted представляет статус задачи в завершенном состоянии
	TaskStatusCompleted TaskStatus = "completed"
	// TaskStatusFailed представляет статус задачи в состоянии ошибки
	TaskStatusFailed TaskStatus = "failed"
)

type ItemStatus string

const (
	// ItemStatusQueued представляет статус элемента в ожидании
	ItemStatusQueued ItemStatus = "queued"
	// ItemStatusDownloading представляет статус элемента в процессе загрузки
	ItemStatusDownloading ItemStatus = "downloading"
	// ItemStatusDone представляет статус элемента в завершенном состоянии
	ItemStatusDone ItemStatus = "done"
	// ItemStatusError представляет статус элемента в состоянии ошибки
	ItemStatusError ItemStatus = "error"
)

// Task представляет задачу
type Task struct {
	ID        TaskID     `json:"id"`
	CreatedAt time.Time  `json:"created_at"`
	Status    TaskStatus `json:"status"`
	Items     []Item     `json:"items"`
}

// Item представляет элемент задачи
type Item struct {
	URL            string     `json:"url"`
	FileName       string     `json:"file_name"`
	Status         ItemStatus `json:"status"`
	Attempts       int        `json:"attempts"`
	ErrorMessage   string     `json:"error_message,omitempty"`
	SizeExpected   int64      `json:"size_expected,omitempty"`
	SizeDownloaded int64      `json:"size_downloaded"`
	StartedAt      *time.Time `json:"started_at,omitempty"`
	CompletedAt    *time.Time `json:"completed_at,omitempty"`
}

// Возвращает детерминированное имя файла из URL, сохраняя расширение, если возможно
func DeriveDeterministicFileName(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err == nil {
		base := path.Base(u.Path)
		if base != "/" && base != "." && base != "" && !strings.Contains(base, "?") {
			// нормализуем имя хешированием полного URL и сохраняем расширение
			ext := path.Ext(base)
			if len(ext) > 0 && len(ext) <= 10 {
				h := sha256.Sum256([]byte(rawURL))
				return hex.EncodeToString(h[:16]) + ext
			}
		}
	}
	h := sha256.Sum256([]byte(rawURL))
	return hex.EncodeToString(h[:16])
}
