# syntax=docker/dockerfile:1
#
# cratecli as a container: `cratecli serve` by default, but any command works
# (docker run cratecli from hardwax --fast). A static Go binary on a
# distroless base: no shell, no package manager, runs as a non-root user.

# ---- build ----------------------------------------------------------------
FROM golang:1.27 AS build
WORKDIR /src
# Dependencies first, so editing code doesn't re-download them.
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
# CGO off: the SQLite driver is pure Go, so the binary needs no C library.
# timetzdata embeds time zones, so digests.yaml's timezone always works.
RUN CGO_ENABLED=0 go build -trimpath -tags timetzdata \
      -ldflags "-s -w -X github.com/williamdunstanmorris/cratecli/internal/cli.Version=${VERSION}" \
      -o /out/cratecli ./cmd/cratecli \
 && mkdir -p /out/data /out/config

# ---- run ------------------------------------------------------------------
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/cratecli /usr/local/bin/cratecli
# Writable for the database; owned by the image's non-root user (65532).
COPY --from=build --chown=65532:65532 /out/data /data
COPY --from=build --chown=65532:65532 /out/config /config
ENV CRATECLI_CONFIG_DIR=/config \
    CRATECLI_LIBRARY=/data/library.db \
    CRATECLI_ADDR=:8080
USER nonroot
EXPOSE 8080
VOLUME ["/data"]
ENTRYPOINT ["cratecli"]
CMD ["serve"]
