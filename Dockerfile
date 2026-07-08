FROM golang:1.25-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o gateway ./cmd/gateway

FROM alpine:3.20
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=builder /app/gateway .
COPY --from=builder /app/web ./web
COPY --from=builder /app/docs ./docs

EXPOSE 8080
CMD ["./gateway"]
