# Build and run dreamreader-sync. The build context must be the hertz-games
# PARENT directory so the cross-repo replace resolves:
#   replace github.com/hertz-iam/authmw-go => ../hertz-iam/packages/authmw-go
#
#   docker build -f dreamreader-sync/Dockerfile -t dreamreader-sync .
#
# (run from I:\Web\hertz-games)
FROM golang:1.26-alpine AS build
WORKDIR /src
ARG GOPROXY=https://goproxy.cn,direct
ENV GOPROXY=$GOPROXY
ENV GOFLAGS=-mod=mod
ENV CGO_ENABLED=0
# Sibling package the module replaces to (the IAM JWT validator). Placed at
# /hertz-iam/packages/authmw-go so /src/../hertz-iam/... resolves.
COPY hertz-iam/packages/authmw-go /hertz-iam/packages/authmw-go
COPY dreamreader-sync /src
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
