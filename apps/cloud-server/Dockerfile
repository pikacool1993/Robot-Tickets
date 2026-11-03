FROM golang:1.24 AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/cloud-server ./cmd/server

FROM debian:bookworm-slim

RUN useradd --system --uid 10001 --no-create-home appuser
WORKDIR /app
COPY --from=builder /out/cloud-server /app/cloud-server
COPY migrations /app/migrations
USER appuser
EXPOSE 8080
ENTRYPOINT ["/app/cloud-server"]
