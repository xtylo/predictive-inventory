# syntax=docker/dockerfile:1

# ---- Build stage: full Go toolchain, discarded after compiling ----
FROM golang:1.27-alpine AS build
WORKDIR /src

# Copy only the dependency manifests first. Docker caches this layer, so
# dependencies are downloaded again only when go.mod or go.sum change.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# CGO_ENABLED=0 produces a fully static binary with no libc dependency.
# -trimpath and -ldflags="-s -w" strip local paths and debug symbols.
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/api ./cmd/api

# ---- Runtime stage: just the binary on a minimal base image ----
# Distroless "static" has no shell and no package manager; it ships only
# CA certificates and timezone data, and runs as a non-root user.
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /out/api /api

# Disable Gin debug logging in the container image.
ENV GIN_MODE=release
EXPOSE 8080

ENTRYPOINT ["/api"]
