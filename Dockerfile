# syntax=docker/dockerfile:1
# buildlens-server: static Go build -> distroless, non-root.
# Image tags and digests checked on 2026-10-04 (docs/study/P1-design.md, section 9).

FROM golang:1.27.1-alpine3.24@sha256:8a5910f31396cd4d89662f56c68b3ae31d374308270a1c3bd96672ee5ed43414 AS build
WORKDIR /src

# Dependencies first, so this layer is cached until go.mod or go.sum change.
COPY go.mod go.sum ./
RUN go mod download

COPY cmd/ cmd/
COPY internal/ internal/
COPY migrations/ migrations/

# CGO_ENABLED=0: a fully static binary that needs no libc, so it runs on distroless/static.
# -trimpath drops local paths; -s -w drops debug symbols to shrink the binary.
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/buildlens-server ./cmd/buildlens-server

FROM gcr.io/distroless/static-debian13:nonroot@sha256:e2e927ec666bae08560abb3c55d0659eceabb657f56b6782ab500a9fc7f555e3
COPY --from=build /out/buildlens-server /usr/local/bin/buildlens-server
# The :nonroot image runs as uid 65532; stated again so it is visible here.
USER 65532:65532
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/buildlens-server"]
