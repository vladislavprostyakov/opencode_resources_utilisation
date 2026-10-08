# opencode-resources-utilisation

Сервис мониторинга утилизации ресурсов Kubernetes для namespace `opencode-agents`.

- **Backend** — сервис на Go, использует официальный клиент `k8s.io/client-go`
  и `k8s.io/metrics` (metrics-server).
- **Frontend** — одностраничное приложение в браузере (HTML/JS/CSS), встроенное
  в бинарник через `go:embed`. Все данные backend отдаёт в формате **JSON**,
  который парсится на стороне браузера.

## Возможности

После выбора пользователя (список формируется парсингом имён подов) отображается:

1. Имена подов выбранного пользователя (может быть несколько).
2. request / limit памяти.
3. request / limit CPU.
4. request / limit Ephemeral Storage.
5. Квота, установленная для namespace.
6. Сколько осталось от квоты namespace.
7. Процент утилизации квоты подами конкретного пользователя.
8. Сколько всего доступно CPU / памяти в кластере (allocatable).
9. Сколько осталось CPU / памяти в кластере после вычета request-запросов.
10. Сколько PVC пользователь запросил и процент утилизации.

По каждому поду:

1. Дата создания / время работы (uptime).
2. Количество перезапусков и причины (в понятной формулировке).
3. Живые графики утилизации CPU, памяти и ephemeral storage (обновление каждые 5 сек).
4. Последние события кластера по поду.
5. Логи пода (настраиваемое количество строк, опция «все контейнеры»).

## Имя пользователя из имени пода

Формат имени пода:

```
opencode-asmirnov-dfnx-ru-6f56465855-tbhlw
└─┬─┘ └──┬───┘ └───┬────┘ └──────────┬─────────┘
prefix  user   mail-suffix   hash (добавляется k8s)
```

Имя пользователя — **второе поле** по разделителю `-` (после префикса).
Префикс настраивается в `config.yaml` (`kubernetes.pod_prefix`).

## Конфигурация

`config.yaml`:

```yaml
server:
  host: "0.0.0.0"
  port: 8080
kubernetes:
  namespace: "opencode-agents"   # namespace с подами
  pod_prefix: "opencode"         # общий префикс имён подов
  kubeconfig: ""                 # путь к kubeconfig; пусто = in-cluster / ~/.kube/config
```

Подключение к кластеру:
- если `kubernetes.kubeconfig` задан — используется этот файл;
- иначе сначала пробуем **in-cluster** (сервис работает внутри кластера);
- иначе `~/.kube/config`.

## Сборка и запуск

```bash
go mod tidy
go build -o opencode-resources.exe .
./opencode-resources.exe -config config.yaml
```

Откройте `http://localhost:8080`.

Флаг `-config` — путь к файлу конфигурации (по умолчанию `config.yaml`).

## HTTP API (JSON)

| Метод | Путь | Описание |
|-------|------|----------|
| GET | `/api/users` | Список пользователей: `{"users": [...]}` |
| GET | `/api/user/{user}` | Полная информация по пользователю (поды, квота, кластер, PVC) |
| GET | `/api/user/{user}/pods/{pod}/metrics` | Текущие метрики пода (CPU/память/ephemeral) |
| GET | `/api/user/{user}/pods/{pod}/logs?tail=200&all=true` | Логи пода |
| GET | `/api/cluster` | Сводка по ресурсам кластера |

## Зависимости и допущения

- **Графики** строятся на основе **текущих** значений из metrics-server
  (история накапливается на фронте опросом каждые 5 сек). Для истории «из коробки»
  нужен metrics-server в кластере. Если он недоступен — метрики и графики
  отображаются как недоступные, остальная информация работает.
- **Утилизация PVC** оценивается приближённо: ephemeral storage usage пода,
  делённый на ёмкость PVC (точное использование тома требует node/volume-метрик).
- Для просмотра логов и событий нужны соответствующие RBAC-права
  (`get/list pods, pods/log, events, persistentvolumeclaims, resourcequotas, nodes`
  и доступ к metrics API).

## Структура проекта

```
main.go                     — точка входа
config.yaml                 — конфигурация
internal/config             — загрузка конфигурации
internal/k8s                — работа с Kubernetes (поды, квоты, кластер, метрики, логи, PVC)
internal/api                — HTTP-сервер + JSON API + встроенный frontend
internal/api/web            — frontend (index.html, app.js, style.css)
```
