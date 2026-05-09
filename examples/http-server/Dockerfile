# Stage 1: build a static binary.
FROM golang:1.23-alpine AS build

WORKDIR /src
COPY go.mod .
COPY main.go .

RUN CGO_ENABLED=0 GOOS=linux go build \
      -trimpath \
      -ldflags="-s -w" \
      -o /out/server \
      .

# Stage 2: minimal runtime image.
FROM alpine:3.20

RUN apk add --no-cache wget

COPY --from=build /out/server /usr/local/bin/server

EXPOSE 8080

USER nobody

ENTRYPOINT ["/usr/local/bin/server"]
