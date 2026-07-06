FROM golang:1.25-alpine AS builder

RUN apk --no-cache add git nodejs npm ca-certificates tzdata
RUN npm install -g pnpm@9.15.3
RUN go install github.com/knadh/stuffbin/...@latest

WORKDIR /src
COPY . .

RUN set -eux; \
    VERSION="$(git describe --tags --abbrev=0 2>/dev/null || echo v0.0.0)"; \
    COMMIT="$(git rev-parse --short HEAD 2>/dev/null || echo dev)"; \
    BUILD_DATE="$(date -u +%Y-%m-%dT%H:%M:%SZ)"; \
    BUILDSTR="${VERSION} (#${COMMIT} ${BUILD_DATE})"; \
    cd frontend; \
    pnpm install --frozen-lockfile; \
    VITE_APP_VERSION="$VERSION" pnpm build:main; \
    VITE_APP_VERSION="$VERSION" pnpm build:widget; \
    cd ..; \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -a \
      -ldflags="-X 'main.buildString=${BUILDSTR}' -X 'main.versionString=${VERSION}' -X 'github.com/abhinavxd/libredesk/internal/version.Version=${VERSION}' -s -w" \
      -o /src/libredesk ./cmd; \
    /go/bin/stuffbin -a stuff -in /src/libredesk -out /src/libredesk frontend/dist i18n schema.sql static

FROM alpine:3.18

RUN apk --no-cache add ca-certificates tzdata

WORKDIR /libredesk

COPY --from=builder /src/libredesk .
COPY config.sample.toml config.toml

EXPOSE 9000

CMD ["./libredesk"]
