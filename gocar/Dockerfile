# Build stage
FROM golang:1.25-alpine AS builder

WORKDIR /app

# Install git for module downloads
RUN apk add --no-cache git

# Copy Go module files and download dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy all source code
COPY . .

# Build the binary
RUN CGO_ENABLED=0 GOOS=linux go build -o gocar .

# Final stage - minimal image
FROM alpine:latest

RUN apk add --no-cache ca-certificates

WORKDIR /root/

# Copy only the binary
COPY --from=builder /app/gocar .

# Run the server
CMD ["./gocar"]