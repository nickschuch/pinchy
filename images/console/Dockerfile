# Build stage: compile the pinchy binary from source.
FROM golang:1.26-alpine AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags '-s -w' -o /pinchy ./cmd/pinchy

# Runtime stage: minimal image with wget for the healthcheck.
FROM alpine:3.23

RUN apk add --no-cache wget

COPY --from=builder /pinchy /usr/local/bin/pinchy

# The console server listens on :8080 and exposes /healthz for Traefik.
EXPOSE 8080

ENTRYPOINT ["pinchy", "console", "serve"]
