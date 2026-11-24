FROM golang:1.24-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/sed-worker ./cmd/worker

FROM alpine:3.21
RUN adduser -D -H -u 10001 appuser
USER appuser
WORKDIR /app
COPY --from=builder /out/sed-worker /usr/local/bin/sed-worker
ENTRYPOINT ["/usr/local/bin/sed-worker"]
