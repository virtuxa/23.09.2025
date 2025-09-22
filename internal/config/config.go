package config

import (
	"bufio"
	"os"
	"strconv"
	"strings"
)

// Конфигурация приложения
type Config struct {
	Port           string
	DataDir        string
	StateDir       string
	Workers        int
	RetryMax       int
	RetryBackoffMs int
}

// Загрузка конфигурации из переменных окружения и .env файла
func Load() *Config {
	// Загрузка .env файла, если он существует
	loadEnvFile(".env")

	return &Config{
		Port:           getenv("PORT", "8080"),
		DataDir:        getenv("DATA_DIR", "data"),
		StateDir:       getenv("STATE_DIR", "var/state"),
		Workers:        getenvInt("WORKERS", 4),
		RetryMax:       getenvInt("RETRY_MAX", 3),
		RetryBackoffMs: getenvInt("RETRY_BACKOFF_MS", 500),
	}
}

// Возвращает значение переменной окружения или значение по умолчанию
func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// Возвращает значение переменной окружения как int или значение по умолчанию
func getenvInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return def
}

// Загружает .env файл, если он существует
func loadEnvFile(filename string) {
	file, err := os.Open(filename)
	if err != nil {
		return // .env файл не существует, это нормально
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			key := strings.TrimSpace(parts[0])
			value := strings.TrimSpace(parts[1])
			// Удаление кавычек, если они присутствуют
			if len(value) >= 2 && ((value[0] == '"' && value[len(value)-1] == '"') ||
				(value[0] == '\'' && value[len(value)-1] == '\'')) {
				value = value[1 : len(value)-1]
			}
			os.Setenv(key, value)
		}
	}
}
