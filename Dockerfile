
# Build stage
FROM golang:1.22-alpine AS build
WORKDIR /src
RUN apk add --no-cache git ca-certificates
COPY . .
ENV CGO_ENABLED=0
RUN go build -trimpath -ldflags="-s -w" -o /out/persian-currency-bot ./cmd/bot

# Runtime stage
FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=build /out/persian-currency-bot /usr/local/bin/persian-currency-bot
# Non-root user
RUN adduser -D -H -u 10001 pcb
USER pcb
ENTRYPOINT ["/usr/local/bin/persian-currency-bot"]
CMD ["-config","/app/config.json"]
