FROM golang:1.24-alpine AS builder
WORKDIR /src
COPY go.mod ./
COPY cmd ./cmd
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/mock-external ./cmd/mock-external

FROM alpine:3.21
RUN adduser -D -H -u 10001 appuser
USER appuser
WORKDIR /app
COPY --from=builder /out/mock-external /usr/local/bin/mock-external
EXPOSE 8092
ENTRYPOINT ["/usr/local/bin/mock-external"]
