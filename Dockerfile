# Vercel control plane. The persistent Tesla mTLS listener is built from
# Dockerfile.railway because it requires a stable, long-lived TCP endpoint.
FROM golang:1.26-bookworm AS build

WORKDIR /src
COPY go.mod go.sum ./
COPY cmd/controlplane ./cmd/controlplane

RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /controlplane ./cmd/controlplane

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /

COPY --from=build --chown=nonroot:nonroot /controlplane /controlplane

ENV HOST=0.0.0.0
EXPOSE 8080

ENTRYPOINT ["/controlplane"]
