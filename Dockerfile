# Build and run dreamreader-sync. Fully self-contained — the build context is
# this repo alone (the IAM validator is vendored under internal/authmw), so it
# builds regardless of the checkout directory name and needs no sibling repos:
#
#   docker build -t dreamreader-sync .
FROM golang:1.26-alpine AS build
WORKDIR /src
ARG GOPROXY=https://goproxy.cn,direct
ENV GOPROXY=$GOPROXY
ENV GOFLAGS=-mod=mod
ENV CGO_ENABLED=0
COPY . .
RUN go build -trimpath -ldflags="-s -w" -o /out/dreamreader-sync ./cmd/dreamreader-sync

FROM alpine:3.20 AS run
# ca-certificates lets the JWKS cache fetch the provider key set over HTTPS.
RUN apk add --no-cache ca-certificates \
	&& adduser -D -u 10001 app \
	&& mkdir -p /data && chown app /data
USER app
WORKDIR /app
COPY --from=build /out/dreamreader-sync /app/dreamreader-sync
ENV DREAMSYNC_HTTP_ADDR=:8090
ENV DREAMSYNC_DB_PATH=/data/dreamsync.db
VOLUME ["/data"]
EXPOSE 8090
ENTRYPOINT ["/app/dreamreader-sync"]
