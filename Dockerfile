# Stage 1: builder
FROM golang:1.26-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /hsd ./cmd/hsd/

# Stage 2: runtime
FROM alpine:3.21
RUN apk add --no-cache ca-certificates
COPY --from=builder /hsd /usr/local/bin/hsd
ENTRYPOINT ["/usr/local/bin/hsd"]
