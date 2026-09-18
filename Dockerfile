# syntax=docker/dockerfile:1

FROM golang:1.26-alpine AS build
WORKDIR /src

# Cache module downloads separately from source changes.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
# CGO disabled -> fully static binary, works on scratch/alpine without libc surprises.
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/netra ./cmd/netra

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata && \
    addgroup -S netra && adduser -S netra -G netra

WORKDIR /app
COPY --from=build /out/netra /app/netra

RUN mkdir -p /data && chown -R netra:netra /data
VOLUME ["/data"]

USER netra
ENV DATA_DIR=/data
EXPOSE 8080

ENTRYPOINT ["/app/netra"]
