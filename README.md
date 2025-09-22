# Task Download Service (Go)
## Запуск
```bash
go run ./cmd/server
```
## HTTP API
- `POST /tasks`
  - Тело:
    ```json
    {"urls": ["https://../.zip", "https://../.jpg"]}
    ```
  - Ответ `201`:
    ```json
    {"id": "<uuid>"}
    ```
- `GET /tasks`
  - Ответ `200`: список кратких сведений по задачам.
- `GET /tasks/{id}`
  - Ответ `200`: подробный статус задачи, прогресс по каждому файлу.

## Примеры
```bash
# Создать задачу
curl -X POST http://localhost:8080/tasks \
  -H 'Content-Type: application/json' \
  -d '{"urls":["https://../.."]}'

# Получить все задачи
curl http://localhost:8080/tasks

# Получить задачу по id
curl http://localhost:8080/tasks/<id>
```