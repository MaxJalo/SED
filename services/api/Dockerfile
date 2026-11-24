FROM golang:1.24-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/sed-api ./cmd/api

FROM alpine:3.21
RUN apk add --no-cache su-exec && adduser -D -H -u 10001 appuser \
    && printf '%s\n' '#!/bin/sh' 'mkdir -p /var/lib/sed/files' 'chown appuser:appuser /var/lib/sed/files' 'exec su-exec appuser /usr/local/bin/sed-api' > /entrypoint.sh \
    && chmod +x /entrypoint.sh
COPY --from=builder /out/sed-api /usr/local/bin/sed-api
EXPOSE 8080
ENTRYPOINT ["/entrypoint.sh"]
