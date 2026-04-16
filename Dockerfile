# Stage 1: Build frontend
FROM node:20-alpine AS frontend-builder
WORKDIR /app/frontend
COPY frontend/package.json frontend/package-lock.json* ./
RUN npm install
COPY frontend/ ./
RUN npm run build

# Stage 2: Build backend
FROM golang:1.25-alpine AS backend-builder
RUN apk add --no-cache gcc musl-dev
WORKDIR /app
COPY backend/go.mod backend/go.sum* ./
RUN go mod download
COPY backend/ ./
RUN CGO_ENABLED=0 go build -o /app/server ./cmd/server

# Stage 3: Production image
FROM alpine:3.19
RUN apk add --no-cache ca-certificates tzdata git
WORKDIR /app

COPY --from=backend-builder /app/server ./server
COPY --from=frontend-builder /app/frontend/dist ./frontend/dist

RUN mkdir -p /app/data

ENV PORT=18765
ENV DATABASE_URL=postgres://anpm:anpm@postgres:5432/anpm?sslmode=disable
ENV FRONTEND_DIR=/app/frontend/dist

EXPOSE 18765

CMD ["./server"]
