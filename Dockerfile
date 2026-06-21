# ── build ────────────────────────────────────────────────────────────────────
FROM golang:1.23 AS builder
WORKDIR /app
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o cerebro .

# ── runtime ──────────────────────────────────────────────────────────────────
FROM gcr.io/distroless/static-debian12
COPY --from=builder /app/cerebro /cerebro
EXPOSE 8090
ENTRYPOINT ["/cerebro"]
