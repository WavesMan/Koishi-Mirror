# Build frontend
FROM node:21-alpine AS frontend-builder
WORKDIR /app/ui
COPY ui/package.json ui/pnpm-lock.yaml* ./
RUN npm install -g pnpm && pnpm install
COPY ui/ .
RUN pnpm run build

# Build backend
FROM golang:1.23-alpine AS backend-builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o npm-mirror ./cmd/main.go

# Final stage
FROM alpine:latest
WORKDIR /app
RUN apk --no-cache add ca-certificates tzdata

# Copy binary
COPY --from=backend-builder /app/npm-mirror .

# Copy frontend assets (backend serves them from ./ui/dist)
COPY --from=frontend-builder /app/ui/dist ./ui/dist

# Expose port
EXPOSE 8080

# Run
CMD ["./npm-mirror"]
