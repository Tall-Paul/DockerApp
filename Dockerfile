# Use the official Golang image to build the application
FROM golang:1.22-alpine AS builder

# Set the working directory
WORKDIR /app

# Copy the go.mod file
COPY go.mod ./

# Download dependencies
RUN go mod download

# Copy the entire source code
COPY . .

# Build the application
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o docker-lister .

# Use a minimal base image for the final image
FROM alpine:latest

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
