# Use the official Golang image to build the application
FROM golang:1.24-alpine AS builder

# Install build dependencies for CGO and SQLite
RUN apk add --no-cache gcc musl-dev sqlite-dev

# Set the working directory
WORKDIR /app

# Copy go.mod and go.sum for dependency resolution
COPY go.mod go.sum ./

# Copy the entire source code
COPY . .

# Download dependencies
RUN go mod download

# Build the application with CGO enabled for SQLite support
RUN CGO_ENABLED=1 GOOS=linux go build -a -installsuffix cgo -o docker-lister .

# Use a minimal base image for the final image
FROM alpine:latest

# Install runtime dependencies for SQLite
RUN apk add --no-cache sqlite-libs

# Set the working directory
WORKDIR /root/

# Copy the built binary from the builder stage
COPY --from=builder /app/docker-lister .

# Copy the templates directory
COPY templates ./templates

# Expose port 8080 for the web server
EXPOSE 8080

# Run the application
CMD ["./docker-lister"]
