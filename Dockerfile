# Multi-stage build
FROM golang:1.26.1-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o logviewer .

FROM gcr.io/distroless/static-debian11
LABEL org.opencontainers.image.source="https://github.com/bascanada/logviewer"
COPY --from=builder /app/logviewer /logviewer
ENTRYPOINT ["/logviewer"]
CMD ["--help"]
