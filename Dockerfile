FROM node:22-alpine AS frontend-builder
WORKDIR /app/frontend
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci
COPY frontend/ ./
RUN npm run build

FROM golang:1.25 AS go-builder
WORKDIR /app
ENV GOPROXY=https://goproxy.cn,direct
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=frontend-builder /app/internal/webui/static ./internal/webui/static
RUN CGO_ENABLED=0 go build -o pve-manage ./cmd/cloud-manage/

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates && rm -rf /var/lib/apt/lists/*
WORKDIR /app
COPY --from=go-builder /app/pve-manage /usr/local/bin/pve-manage
RUN mkdir -p /app/config
EXPOSE 8080
ENTRYPOINT ["pve-manage"]
