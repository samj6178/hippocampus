FROM golang:1.23-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags "-s -w -X main.version=$(git describe --tags --always --dirty 2>/dev/null || echo dev)" \
    -o /hippocampus ./cmd/hippocampus/

FROM alpine:3.19
RUN apk add --no-cache ca-certificates tzdata wget \
    && adduser -D -u 1000 hippocampus
COPY --from=builder /hippocampus /usr/local/bin/hippocampus
COPY config.docker.json /etc/hippocampus/config.json
COPY migrations/ /migrations/

RUN mkdir -p /data && chown hippocampus:hippocampus /data
USER hippocampus
VOLUME /data

EXPOSE 8080 9090
ENTRYPOINT ["hippocampus", "-config", "/etc/hippocampus/config.json", "-migrations", "/migrations"]
