FROM golang:1.23-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git

# Set working directory
WORKDIR /app

# Copy go.mod and go.sum files (if they exist)
COPY go.mod go.sum* ./

# Download dependencies
RUN go mod download

# Copy source code
COPY *.go ./

# Build the application
RUN CGO_ENABLED=0 GOOS=linux go build -o telegram-file-saver

# Create a minimal production image
FROM alpine:latest

# Add necessary runtime dependencies
RUN apk --no-cache add ca-certificates tzdata

# Create directories for file storage
RUN mkdir -p /app/files

# Copy the compiled binary
COPY --from=builder /app/telegram-file-saver /app/

# Set working directory
WORKDIR /app

# Expose volume for persistent file storage
VOLUME ["/app/files"]

# Run the application
CMD ["/app/telegram-file-saver"]