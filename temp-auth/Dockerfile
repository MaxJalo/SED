FROM golang:1.24-alpine AS builder
WORKDIR /src
COPY go.mod ./
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/temp-auth ./cmd/temp-auth

FROM alpine:3.21
RUN adduser -D -H -u 10001 appuser
USER appuser
WORKDIR /app
COPY --from=builder /out/temp-auth /usr/local/bin/temp-auth
EXPOSE 8091
ENTRYPOINT ["/usr/local/bin/temp-auth"]
