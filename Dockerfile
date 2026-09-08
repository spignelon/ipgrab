# syntax=docker/dockerfile:1

FROM golang:1.26-alpine AS build
WORKDIR /src

# Cache module downloads separately from source changes.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
# CGO disabled -> fully static binary, works on scratch/alpine without libc surprises.
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/ipgrab ./cmd/ipgrab

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata && \
    addgroup -S ipgrab && adduser -S ipgrab -G ipgrab

WORKDIR /app
COPY --from=build /out/ipgrab /app/ipgrab

RUN mkdir -p /data && chown -R ipgrab:ipgrab /data
VOLUME ["/data"]

USER ipgrab
ENV DATA_DIR=/data
EXPOSE 8080

ENTRYPOINT ["/app/ipgrab"]
