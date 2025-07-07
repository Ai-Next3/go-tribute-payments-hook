# Этап 1: Сборка приложения
FROM golang:1.23 AS builder

WORKDIR /build
COPY app/go.mod app/go.sum ./
RUN go mod download

# Копируем все файлы приложения, включая папку session
COPY ./app ./app

# Собираем приложение
RUN cd app && go build -o /tribute-hook .

# Этап 2: Создание финального, легковесного образа
FROM gcr.io/distroless/static-debian12

# Создаем рабочую директорию
WORKDIR /app

# Копируем скомпилированное приложение из этапа сборки
COPY --from=builder /tribute-hook .

# Запускаем приложение
CMD ["/app/tribute-hook"]