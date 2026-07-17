# Build stage
FROM golang:1.26-alpine AS builder

RUN apk add --no-cache build-base

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=1 GOOS=linux go build -o /bot ./cmd/bot

# Run stage
FROM alpine:3.21

RUN apk add --no-cache ca-certificates sqlite-libs

WORKDIR /app
RUN mkdir -p /app/data
COPY --from=builder /bot .

VOLUME ["/app/data"]
CMD ["./bot"]
