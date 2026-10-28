FROM node:26.12.1-alpine@sha256:40cd011b55415bc894e45b02fdd5a5dfde056921b7d23a64c74f80497af48797 AS ui
WORKDIR /src/web
COPY web/package*.json ./
RUN npm ci --ignore-scripts
COPY web/index.html web/tsconfig.json web/vite.config.ts ./
COPY web/src ./src
RUN npm run build

FROM golang:1.27.2-alpine@sha256:f92b6ef800e499660581efdabdf25d9d817a9d124eaf900924f0504e7e27e12d AS build
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
