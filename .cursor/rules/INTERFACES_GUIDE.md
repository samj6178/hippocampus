# Генерация интерфейсов по Best Practices Go

## Подход: "Accept interfaces, return structs"

**Правило:** Интерфейсы определяются там, где они используются (в пакете потребителя), а не там, где реализуются.

## Структура

```
kafka_project/
  interfaces.go          ← интерфейсы для kafka_project
  project_management.go  ← использует интерфейсы

kafka_data/
  interfaces.go          ← интерфейсы для kafka_data
  data_management.go

kafka_model/
  interfaces.go          ← интерфейсы для kafka_model
  model_management.go

storage/
  metadatastore.go       ← реализация БЕЗ интерфейса
  minio.go               ← реализация БЕЗ интерфейса
```

## Пример интерфейса

```go
// kafka_project/interfaces.go
package kafka

// MetadataStore defines the contract for metadata storage operations
// used by project management consumer.
//
//go:generate mockery --name=MetadataStore --output=../internal/mocks --outpkg=mocks --filename=project_metadata_store_mock.go
type MetadataStore interface {
    EnsureClient() (interface{}, error)
    GenerateID(entityType string) (int64, error)
    SaveEntityWithID(entityType string, value map[string]interface{}, id string) error
    DeleteEntity(entityType string, id string) error
    AcquireRollbackLock(projectID int64) (bool, error)
    ReleaseRollbackLock(projectID int64) error
    IsRollbackCompleted(projectID int64) (bool, error)
}
```

## Использование

```go
// kafka_project/project_management.go
func StartProjectManagementConsumer(
    ctx context.Context,
    cfg *config.Config,
    metadataStore MetadataStore,  // ← интерфейс
    minioClient MinioClient,      // ← интерфейс
) error {
    // ...
}
```

## Генерация моков

```bash
# В каждом kafka-пакете
go generate ./kafka_project/...
go generate ./kafka_data/...
go generate ./kafka_model/...
```
