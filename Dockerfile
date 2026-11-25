FROM node:26.11.1-alpine@sha256:143494b1da2945f061539253adc65e4f1569ddf07da2d384c022c791a9d90a4a AS ui
WORKDIR /src/web
COPY web/package*.json ./
RUN npm ci --ignore-scripts
COPY web/index.html web/tsconfig.json web/vite.config.ts ./
COPY web/src ./src
RUN npm run build

FROM golang:1.27.4-alpine@sha256:70bd7ddea8b6cbe5d6943cde279d6b67feb10cc924fb9100d1d8757c1575b280 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
COPY web/embed.go ./web/embed.go
COPY --from=ui /src/web/dist ./web/dist
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /seaweed-zip ./cmd/seaweed-zip \
    && mkdir -p /rootfs/data && chown 10001:10001 /rootfs/data

FROM scratch
LABEL org.opencontainers.image.source="https://github.com/hoomoli/seaweed-zip" \
      org.opencontainers.image.licenses="MIT" \
      org.opencontainers.image.title="Seaweed Zip"
COPY --from=build /seaweed-zip /usr/local/bin/seaweed-zip
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build --chown=10001:10001 /rootfs/data /data
COPY LICENSE THIRD_PARTY_NOTICES.txt /usr/share/licenses/seaweed-zip/
USER 10001:10001
ENV SEAWEED_ZIP_DATA=/data
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 CMD ["/usr/local/bin/seaweed-zip", "healthcheck"]
ENTRYPOINT ["/usr/local/bin/seaweed-zip"]
