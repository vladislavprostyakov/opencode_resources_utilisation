# syntax=docker/dockerfile:1

# ============================================================
# Этап 1: сборка
# ============================================================
FROM --platform=linux/amd64 golang:1.25-alpine AS builder

WORKDIR /build

# Кэшируем зависимости отдельно от исходного кода
COPY go.mod go.sum ./
RUN go mod download

COPY main.go ./
COPY internal/ ./internal/

# Статический бинарник для linux/amd64
ARG TARGETOS=linux
ARG TARGETARCH=amd64
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w" -o /out/opencode-resources .

# ============================================================
# Этап 2: рантайм
# ============================================================
FROM --platform=linux/amd64 alpine:3.20

# Утилиты для healthcheck и корректного завершения процесса
RUN apk add --no-cache ca-certificates curl tzdata \
    && addgroup -S app && adduser -S -G app app

WORKDIR /app

COPY --from=builder /out/opencode-resources /app/opencode-resources
COPY config.yaml /app/config.yaml

USER app

ENV CONFIG=/app/config.yaml
EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD curl -fsS http://127.0.0.1:8080/ || exit 1

ENTRYPOINT ["/app/opencode-resources"]
CMD ["-config", "/app/config.yaml"]
