ARG BUILDPLATFORM
ARG TARGETPLATFORM
FROM --platform=${BUILDPLATFORM} golang:1.26.5-alpine3.24@sha256:0178a641fbb4858c5f1b48e34bdaabe0350a330a1b1149aabd498d0699ff5fb2 AS builder

ARG VERSION=v0.0.0
ARG COMMIT=dev
ARG BUILD_DATE=unknown
ARG TARGETOS=linux
ARG TARGETARCH=amd64
ARG TARGETVARIANT

RUN apk --no-cache add git nodejs npm ca-certificates tzdata
RUN npm install -g pnpm@9.15.3
RUN go install github.com/knadh/stuffbin/...@v1.3.0

WORKDIR /src
COPY . .

RUN set -eux; \
	BUILDSTR="${VERSION} (#${COMMIT} ${BUILD_DATE})"; \
	GOARM_VALUE="${TARGETVARIANT#v}"; \
	cd frontend; \
	pnpm install --frozen-lockfile; \
	VITE_APP_VERSION="$VERSION" pnpm build:main; \
	VITE_APP_VERSION="$VERSION" pnpm build:widget; \
	cd ..; \
	CGO_ENABLED=0 GOOS="$TARGETOS" GOARCH="$TARGETARCH" GOARM="$GOARM_VALUE" go build -a -trimpath -buildvcs=false \
	  -ldflags="-X 'main.buildString=${BUILDSTR}' -X 'main.versionString=${VERSION}' -X 'github.com/abhinavxd/libredesk/internal/version.Version=${VERSION}' -s -w" \
	  -o /src/libredesk ./cmd; \
	/go/bin/stuffbin -a stuff -in /src/libredesk -out /src/libredesk frontend/dist i18n schema.sql static

FROM --platform=${TARGETPLATFORM} alpine:3.24.0@sha256:a2d49ea686c2adfe3c992e47dc3b5e7fa6e6b5055609400dc2acaeb241c829f4

RUN apk --no-cache add ca-certificates tzdata \
	&& addgroup -S -g 10001 libredesk \
	&& adduser -S -D -H -u 10001 -G libredesk libredesk \
	&& mkdir -p /libredesk/uploads \
	&& chown libredesk:libredesk /libredesk/uploads \
	&& chmod 0750 /libredesk/uploads

WORKDIR /libredesk

COPY --chown=root:root --from=builder /src/libredesk /libredesk/libredesk
COPY --chown=root:root config.sample.toml /libredesk/config.toml
RUN chmod 0555 /libredesk/libredesk && chmod 0444 /libredesk/config.toml

ENV LIBREDESK_APP__ENV=prod
USER libredesk:libredesk

EXPOSE 9000
STOPSIGNAL SIGTERM

HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 \
	CMD wget -q -O /dev/null http://127.0.0.1:9000/ready || exit 1

CMD ["/libredesk/libredesk"]
