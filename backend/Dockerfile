# --- build ---
# go.mod declara `go 1.26.0`: a imagem precisa ser >= isso, senão o build morre
# com "go.mod requires go >= 1.26.0 (running go 1.25.x; GOTOOLCHAIN=local)".
# 1.25+ também é exigido pelo minio-go/v7 (bump da dep na Fase 3 sprint H).
FROM golang:1.26-alpine AS build
WORKDIR /src

# Cache de dependências: go.sum é gerado aqui na primeira build
COPY go.mod go.sum* ./
RUN go mod download || true

COPY . .
RUN go mod tidy && \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/ndovu-api ./cmd/api && \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/ndovu-writer ./cmd/writer && \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/ndovu-downloader ./cmd/downloader

# --- downloader runtime (yt-dlp + ffmpeg) ---
# A API roda em distroless (sem python/ffmpeg), então a conversão de MP3 vive
# num serviço separado. Use `target: downloader` no compose.
# Fica ANTES do estágio final de propósito: o alvo default do Dockerfile
# continua sendo a API (distroless), usada por api e writer.
FROM python:3.12-slim AS downloader
RUN apt-get update && \
    apt-get install -y --no-install-recommends ffmpeg ca-certificates && \
    rm -rf /var/lib/apt/lists/*
# yt-dlp quebra a cada mudança do YouTube: o rebuild é a atualização.
RUN pip install --no-cache-dir --upgrade yt-dlp
COPY --from=build /out/ndovu-downloader /ndovu-downloader
RUN useradd --system --uid 10001 --create-home ndovu && \
    mkdir -p /data/downloads && chown -R ndovu:ndovu /data
USER ndovu
EXPOSE 8090
ENTRYPOINT ["/ndovu-downloader"]

# --- runtime ---
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/ndovu-api /ndovu-api
COPY --from=build /out/ndovu-writer /ndovu-writer
EXPOSE 8080
USER nonroot
# Default: API. O serviço "writer" do compose sobrescreve o entrypoint.
ENTRYPOINT ["/ndovu-api"]
