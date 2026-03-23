FROM golang:1.23-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags "-s -w -X main.version=$(git describe --tags --always --dirty 2>/dev/null || echo dev)" \
    -o /hippocampus ./cmd/hippocampus/

FROM alpine:3.19
RUN apk add --no-cache ca-certificates tzdata
COPY --from=builder /hippocampus /usr/local/bin/hippocampus
COPY migrations/ /migrations/

EXPOSE 3000 8080 9090
ENTRYPOINT ["hippocampus"]
