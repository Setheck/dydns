FROM golang:alpine AS builder

ARG VERSION=dev
ARG COMMIT=none

ENV TZ="America/Los_Angeles"
ENV CGO_ENABLED=0
WORKDIR /build
COPY go.mod .
COPY go.sum .
RUN go mod download

COPY . .
RUN mkdir -p bin && \
    go build \
      -trimpath \
      -ldflags="-s -w -X main.version=${VERSION} -X main.commit=${COMMIT}" \
      -o /build/bin/ ./...
WORKDIR /app
RUN cp /build/bin/dydns ./dydns

FROM alpine

RUN apk add --no-cache tzdata
ENV TZ="America/Los_Angeles"
RUN mkdir -p /app /data

COPY --chown=65534:0 --from=builder /app /app
USER 65534

EXPOSE 8080

WORKDIR /data
ENTRYPOINT ["/app/dydns"]
