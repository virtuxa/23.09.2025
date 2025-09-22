package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"taskservice/internal/config"
	"taskservice/internal/httpapi"
	"taskservice/internal/manager"
	"taskservice/internal/storage"
)

func main() {
	cfg := config.Load()

	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		log.Fatalf("failed to create data dir: %v", err)
	}
	if err := os.MkdirAll(cfg.StateDir, 0o755); err != nil {
		log.Fatalf("failed to create state dir: %v", err)
	}

	// Инициализация хранилища
	st, err := storage.NewStore(cfg.StateDir)
	if err != nil {
		log.Fatalf("failed to init storage: %v", err)
	}
	defer st.Close()

	// Инициализация менеджера
	mgr, err := manager.NewManager(manager.Config{
		Store:           st,
		DataDir:         cfg.DataDir,
		WorkerCount:     cfg.Workers,
		MaxRetryPerItem: cfg.RetryMax,
		BaseBackoff:     time.Duration(cfg.RetryBackoffMs) * time.Millisecond,
		SnapshotEveryN:  50,
	})
	if err != nil {
		log.Fatalf("failed to init manager: %v", err)
	}

	if err := mgr.Start(); err != nil {
		log.Fatalf("failed to start manager: %v", err)
	}
	defer mgr.StopAndWait(context.Background())

	// Регистрация обработчиков HTTP
	mux := http.NewServeMux()
	httpapi.RegisterHandlers(mux, mgr)

	// Инициализация HTTP сервера
	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	// Запуск HTTP сервера
	go func() {
		log.Printf("HTTP server listening on :%s", cfg.Port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server error: %v", err)
		}
	}()

	// Выход из программы
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
	log.Printf("shutdown signal received")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("server shutdown error: %v", err)
	}

	if err := mgr.StopAndWait(ctx); err != nil {
		log.Printf("manager stop error: %v", err)
	}

	// Сохранение снапшота
	if err := st.SaveSnapshot(); err != nil {
		log.Printf("snapshot save error: %v", err)
	}

	log.Printf("shutdown complete")
}
