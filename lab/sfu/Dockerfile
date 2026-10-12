# The SFU, built from the vendored source in this repository (upstream pion sfu-ws + the ICE patch).
# No upstream image is published for that example, and building here keeps the lab reproducible: the
# version that runs is the version this repository contains, not whatever a tag points at today.
FROM golang:1.27.2-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY internal ./internal
COPY lab/sfu ./lab/sfu
# The example loads its landing page from the working directory at start-up, so a build that forgets
# it produces a container that panics before it serves anything.
RUN CGO_ENABLED=0 go build -trimpath -o /out/sfu ./lab/sfu

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=build /out/sfu /app/sfu
COPY lab/sfu/index.html /app/index.html
USER nonroot
EXPOSE 8080/udp 7882/udp 7883/tcp
ENTRYPOINT ["/app/sfu"]
