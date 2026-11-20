FROM golang:1.24-alpine AS builder
WORKDIR /src
COPY go.mod ./
COPY cmd ./cmd
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/sed-api ./cmd/api

FROM alpine:3.21
RUN adduser -D -H -u 10001 appuser
USER appuser
WORKDIR /app
COPY --from=builder /out/sed-api /usr/local/bin/sed-api
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/sed-api"]
