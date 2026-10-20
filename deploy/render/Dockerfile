# Render Free demo packaging. Build context is the repository root.
FROM golang:1.27.1-bookworm AS build
WORKDIR /src/backend
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ ./
RUN CGO_ENABLED=0 go build -trimpath -o /out/api ./cmd/api \
 && CGO_ENABLED=0 go build -trimpath -o /out/bank-a ./cmd/bank-a \
 && CGO_ENABLED=0 go build -trimpath -o /out/bank-b ./cmd/bank-b \
 && CGO_ENABLED=0 go build -trimpath -o /out/worker ./cmd/worker \
 && CGO_ENABLED=0 go build -trimpath -o /out/demo-host ./cmd/demo-host \
 && CGO_ENABLED=0 go build -trimpath -o /out/migrate ./cmd/migrate \
 && CGO_ENABLED=0 go build -trimpath -o /out/demo-seed ./cmd/demo-seed

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates \
 && rm -rf /var/lib/apt/lists/* \
 && useradd --uid 10001 --create-home app
WORKDIR /app
COPY --from=build /out/ ./bin/
COPY backend/migrations/ ./migrations/
# Only the checked-in corpus documents; no .env, database dumps or local artifacts.
COPY docs/architecture.md docs/decisions.md ./docs/
COPY docs/runbooks/ ./docs/runbooks/
ENV TX_MIGRATIONS_DIR=/app/migrations
USER 10001
EXPOSE 10000
ENTRYPOINT ["/app/bin/demo-host"]
