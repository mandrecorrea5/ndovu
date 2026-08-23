# --- build ---
# 1.25+ exigido pelo minio-go/v7 (bump da dep na Fase 3 sprint H).
FROM golang:1.25-alpine AS build
WORKDIR /src

# Cache de dependências: go.sum é gerado aqui na primeira build
COPY go.mod go.sum* ./
RUN go mod download || true

COPY . .
RUN go mod tidy && \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/ndovu-api ./cmd/api && \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/ndovu-writer ./cmd/writer

# --- runtime ---
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/ndovu-api /ndovu-api
COPY --from=build /out/ndovu-writer /ndovu-writer
EXPOSE 8080
USER nonroot
# Default: API. O serviço "writer" do compose sobrescreve o entrypoint.
ENTRYPOINT ["/ndovu-api"]
