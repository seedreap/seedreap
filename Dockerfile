# Build stage
FROM golang:1.25-alpine AS builder

WORKDIR /src

# Install git for go mod download
RUN apk add --no-cache git

# Copy go mod files first for caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the binary
ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_DATE=unknown
ARG BUILT_BY=unknown

RUN CGO_ENABLED=0 GOOS=linux go build \
  -ldflags="-s -w \
  -X github.com/seedreap/seedreap/cmd.Version=${VERSION} \
  -X github.com/seedreap/seedreap/cmd.Commit=${COMMIT} \
  -X github.com/seedreap/seedreap/cmd.BuildDate=${BUILD_DATE} \
  -X github.com/seedreap/seedreap/cmd.BuiltBy=${BUILT_BY}" \
  -o /seedreap .

# Runtime stage - using distroless for minimal attack surface
FROM gcr.io/distroless/static:nonroot

LABEL org.opencontainers.image.source="https://github.com/seedreap/seedreap"
LABEL org.opencontainers.image.description="Reap what your seedbox has sown - High-speed parallel transfers from seedboxes"
LABEL org.opencontainers.image.licenses="Apache-2.0"

# Copy binary from builder
COPY --from=builder /seedreap /usr/local/bin/seedreap

# distroless/static:nonroot already runs as nonroot user (uid 65532)
# and includes ca-certificates

EXPOSE 8423

ENTRYPOINT ["seedreap"]
