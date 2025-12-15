FROM golang:alpine AS builder

ENV TZ="America/Los_Angeles"
WORKDIR /build
COPY go.mod .
COPY go.sum .
RUN go mod download

COPY . .
RUN mkdir -p bin && \
    go build -o /build/bin/ ./...
WORKDIR /app
RUN cp /build/bin/dydns ./dydns

FROM alpine

RUN apk add --no-cache tzdata
ENV TZ="America/Los_Angeles"
RUN mkdir -p /app /data

COPY --chown=65534:0 --from=builder /app /app
USER 65534

WORKDIR /data
ENTRYPOINT ["/app/dydns"]
