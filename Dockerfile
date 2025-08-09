# ---------- Stage 1: Build ----------
    FROM golang:1.22 AS builder

    # Install ffmpeg in build stage
    RUN apt-get update && apt-get install -y ffmpeg && rm -rf /var/lib/apt/lists/*
    
    WORKDIR /app
    
    # Copy go.mod and go.sum first for caching
    COPY go.mod go.sum ./
    RUN go mod download
    
    # Copy all source code
    COPY . .
    
    # Build binary
    RUN go build -tags netgo -ldflags="-s -w" -o app .
    
    # ---------- Stage 2: Run ----------
    FROM debian:bullseye-slim
    
    # Install ffmpeg in final container
    RUN apt-get update && apt-get install -y ffmpeg && rm -rf /var/lib/apt/lists/*
    
    WORKDIR /app
    
    # Copy compiled binary and templates
    COPY --from=builder /app/app .
    COPY --from=builder /app/templates ./templates
    
    # Expose port
    EXPOSE 8080
    
    # Start the app
    CMD ["./app"]
    