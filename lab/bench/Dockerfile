# The harness as an image, so a load test can run where a real client runs: inside the cluster.
#
# This exists for a measured reason. The first version of the network-policy fault used the host as
# the client, and the fault never reproduced - not with kind's default CNI and not with Calico -
# because traffic from a pod to the node is not subject to the pod's policy the way traffic between
# two pods is. A fault lab has to put the peer where the fault lives.
FROM golang:1.27.2-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY internal ./internal
COPY cmd ./cmd
COPY testdata ./testdata
RUN CGO_ENABLED=0 go build -trimpath -o /out/bench ./cmd/bench

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=build /out/bench /app/bench
COPY --from=build /src/testdata /app/testdata
USER nonroot
ENTRYPOINT ["/app/bench"]
