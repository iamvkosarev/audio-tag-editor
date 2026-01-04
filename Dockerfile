FROM golang:1.24-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN go install github.com/a-h/templ/cmd/templ@latest
RUN templ generate ./internal/templates

RUN CGO_ENABLED=0 GOOS=linux go build -o /app/bin/api-server ./cmd/api-server

FROM alpine:latest

RUN apk --no-cache add ca-certificates

WORKDIR /app

COPY --from=builder /app/bin/api-server .

EXPOSE 8080

CMD ["./api-server"]

