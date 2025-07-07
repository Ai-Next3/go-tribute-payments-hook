# Этап 1: Сборка приложения
FROM golang:1.23 AS builder

WORKDIR /build
COPY app/go.mod app/go.sum ./
RUN go mod download

# Копируем все файлы приложения, включая папку session
COPY ./app ./app

# Собираем основное приложение
RUN cd app && CGO_ENABLED=0 go build -o /tribute-hook .

# Собираем инструмент для аутентификации
RUN cd app && CGO_ENABLED=0 go build -o /auth-tool ./cmd/auth

# Этап 2: Создание финального, легковесного образа
FROM gcr.io/distroless/static-debian12

# Создаем рабочую директорию
WORKDIR /app

# Копируем скомпилированное приложение из этапа сборки
COPY --from=builder /tribute-hook .

# Копируем инструмент аутентификации
COPY --from=builder /auth-tool .

# ВРЕМЕННО: Запускаем бесконечный сон, чтобы контейнер не падал
# Это позволит нам зайти в терминал и запустить /app/auth-tool вручную
CMD ["sleep", "infinity"]

# ПОСТОЯННО: После аутентификации нужно будет закомментировать строку выше
# и раскомментировать строку ниже, чтобы запустить основное приложение
# CMD ["/app/tribute-hook"]