# The harness and the SFU, built once and run as two containers.
#
# The image is distroless because nothing here needs a shell at runtime: the harness opens UDP
# sockets, sends media and prints a table, and the `image` target asserts that there is no
# /bin/sh inside, because "minimal" that is not checked is a claim rather than a property.
FROM golang:1.27 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# -trimpath: the image should not carry the path of the machine that built it.
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/bench ./cmd/bench \
 && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/sfu ./lab/sfu

FROM gcr.io/distroless/static-debian12
COPY --from=build /out/bench /usr/local/bin/bench
COPY --from=build /out/sfu /usr/local/bin/sfu
# The SFU reads its index.html from the working directory, so the asset has to be in the image
# and the working directory has to be where it is. Without this the container starts, panics with
# "open index.html: no such file or directory", and a restart policy turns that into a loop that
# looks like a crash rather than a missing file.
COPY --from=build /src/lab/sfu/index.html /app/index.html
# The harness reads the sample clip it sends from a path relative to the working directory, the
# same way the SFU reads its page. Both are small, and an image that starts and then fails on a
# missing asset is an image nobody can run.
COPY --from=build /src/testdata /app/testdata
WORKDIR /app
# Running the image with no arguments prints the harness's own usage, which is what `make image`
# asserts before it asserts that there is no shell.
ENTRYPOINT ["/usr/local/bin/bench"]
