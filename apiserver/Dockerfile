# Stage 1: Build the frontend
FROM node:24-alpine AS frontend-builder

WORKDIR /app/frontend

RUN corepack enable

COPY frontend/package.json frontend/yarn.lock ./
RUN yarn install --immutable

COPY frontend/ ./

ARG VITE_BACKEND_VERSION=dev
ENV VITE_BACKEND_VERSION=${VITE_BACKEND_VERSION}

RUN yarn build

# Stage 2: Build the Go binary (embeds the frontend build output)
FROM golang:1.25-alpine AS builder

ARG VERSION=dev
ARG BUILD_NUMBER=0
ARG COMMIT_HASH=unknown

WORKDIR /usr/src/app

COPY apiserver/go.mod apiserver/go.sum ./
RUN go mod download

COPY apiserver/ .
COPY --from=frontend-builder /app/frontend/dist ./frontend/dist

RUN CGO_ENABLED=0 go build -ldflags "-s -w \
    -X taskwiz.app/core/internal/version.Version=${VERSION} \
    -X taskwiz.app/core/internal/version.BuildNumber=${BUILD_NUMBER} \
    -X taskwiz.app/core/internal/version.CommitHash=${COMMIT_HASH}" \
    -o /usr/src/app/task-wizard .

# Stage 3: Create a smaller runtime image
FROM alpine:latest

RUN apk --no-cache add curl ca-certificates libc6-compat

COPY --from=builder /usr/src/app/task-wizard /task-wizard
COPY apiserver/config/config.yaml /config/config.yaml

ENV GIN_MODE="release"

EXPOSE 2021

HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 CMD curl -s --fail http://localhost:2021/ping &> /dev/null || exit 1

CMD ["/task-wizard"]
