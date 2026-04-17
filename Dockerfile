# [PROJECT_NAME] — Multi-Stage Dockerfile
#
# AGENT INSTRUCTION: Replace the build and runtime stages below with the
# appropriate commands for this project's language and framework.
# Keep the multi-stage pattern: builder stage for compiling, runtime stage
# for the final minimal image.

# ============================================================
# Stage 1: Builder
# ============================================================

# --- Go projects ---
# FROM golang:1.24-alpine AS builder
# WORKDIR /src
# COPY go.mod go.sum ./
# RUN go mod download
# COPY . .
# RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w -X main.Version=${VERSION:-dev}" \
#     -o /app ./cmd/server

# --- Python projects ---
# FROM python:3.12-slim AS builder
# WORKDIR /app
# RUN pip install --upgrade pip
# COPY requirements.txt .
# RUN pip install --no-cache-dir -r requirements.txt

# --- Node/TypeScript projects ---
# FROM node:22-alpine AS builder
# WORKDIR /app
# COPY package*.json ./
# RUN npm ci --only=production
# COPY . .
# RUN npm run build

# ============================================================
# Stage 2: Runtime
# ============================================================

# --- Go runtime (distroless — smallest, most secure) ---
# FROM gcr.io/distroless/static-debian12 AS runtime
# COPY --from=builder /app /app
# USER nonroot:nonroot
# EXPOSE 8080
# ENTRYPOINT ["/app"]

# --- Python runtime ---
# FROM python:3.12-slim AS runtime
# WORKDIR /app
# COPY --from=builder /app /app
# RUN addgroup --system app && adduser --system --group app
# USER app
# EXPOSE 8080
# CMD ["python", "-m", "gunicorn", "wsgi:app"]

# --- Node runtime ---
# FROM node:22-alpine AS runtime
# WORKDIR /app
# COPY --from=builder /app/dist ./dist
# COPY --from=builder /app/node_modules ./node_modules
# RUN addgroup -S app && adduser -S app -G app
# USER app
# EXPOSE 8080
# CMD ["node", "dist/server.js"]

# ============================================================
# TEMPORARY PLACEHOLDER — replace with one of the above
# ============================================================
FROM alpine:3.21
RUN echo "Replace this Dockerfile with the appropriate language template above"
CMD ["echo", "Dockerfile not configured — see comments for language-specific templates"]
