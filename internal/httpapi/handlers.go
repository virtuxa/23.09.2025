package httpapi

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"taskservice/internal/manager"
	"taskservice/internal/model"
)

type createTaskRequest struct {
	URLs []string `json:"urls"`
}

type createTaskResponse struct {
	ID string `json:"id"`
}

// Регистрирует обработчики HTTP запросов
func RegisterHandlers(mux *http.ServeMux, mgr *manager.Manager) {
	mux.HandleFunc("/tasks", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			handleCreateTask(w, r, mgr)
		case http.MethodGet:
			handleListTasks(w, r, mgr)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
	mux.HandleFunc("/tasks/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		id := strings.TrimPrefix(r.URL.Path, "/tasks/")
		if id == "" {
			http.NotFound(w, r)
			return
		}
		t, ok := mgr.GetTask(model.TaskID(id))
		if !ok {
			http.NotFound(w, r)
			return
		}
		writeJSON(w, t, http.StatusOK)
	})
}

// Обработчик создания задачи
func handleCreateTask(w http.ResponseWriter, r *http.Request, mgr *manager.Manager) {
	var req createTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if len(req.URLs) == 0 {
		http.Error(w, "urls is required", http.StatusBadRequest)
		return
	}
	id, err := mgr.CreateTask(req.URLs)
	if err != nil {
		log.Printf("create task error: %v", err)
		http.Error(w, "failed to create task", http.StatusInternalServerError)
		return
	}
	writeJSON(w, createTaskResponse{ID: string(id)}, http.StatusCreated)
}

// Обработчик списка задач
func handleListTasks(w http.ResponseWriter, r *http.Request, mgr *manager.Manager) {
	tasks := mgr.ListTasks()
	writeJSON(w, tasks, http.StatusOK)
}

// Вспомогательная функция для сериализации JSON
func writeJSON(w http.ResponseWriter, v interface{}, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}
