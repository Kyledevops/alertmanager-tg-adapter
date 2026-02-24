# Test stage — builds and runs all unit tests
FROM golang:1.26-alpine AS tester

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN go mod tidy
RUN go test -v -count=1 ./...

# Build stage — verifies production binary compiles
FROM golang:1.26-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN go mod tidy
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=linux GOARCH=$TARGETARCH go build -o alertmanager-tg-adapter .

# Final stage
FROM alpine:latest

WORKDIR /app

COPY --from=builder /app/alertmanager-tg-adapter .
COPY --from=builder /app/templates/ ./templates/

EXPOSE 9087

CMD ["./alertmanager-tg-adapter"]
