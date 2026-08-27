# Start by building the application.
FROM golang:1.26-bookworm AS build

# build libsodium (dep of libzmq)
WORKDIR /build
RUN wget https://github.com/jedisct1/libsodium/releases/download/1.0.19-RELEASE/libsodium-1.0.19.tar.gz
RUN tar -xzvf libsodium-1.0.19.tar.gz
WORKDIR /build/libsodium-stable
RUN ./configure --disable-shared --enable-static
RUN make -j`nproc`
RUN make install

# build libzmq (dep of zmq datastore)
WORKDIR /build
RUN wget https://github.com/zeromq/libzmq/releases/download/v4.3.5/zeromq-4.3.5.tar.gz
RUN tar -xvf zeromq-4.3.5.tar.gz
WORKDIR /build/zeromq-4.3.5
RUN ./configure --enable-static --disable-shared --disable-Werror
RUN make -j`nproc`
RUN make install

WORKDIR /go/src/fleet-telemetry

COPY . .
ENV CGO_ENABLED=1
ENV CGO_LDFLAGS="-lstdc++"

RUN make
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /go/bin/healthcheck ./cmd/healthcheck

# hadolint ignore=DL3006
FROM gcr.io/distroless/cc-debian12:nonroot
WORKDIR /
COPY --from=build --chown=nonroot:nonroot /go/bin/fleet-telemetry /fleet-telemetry
COPY --from=build --chown=nonroot:nonroot /go/bin/healthcheck /healthcheck
COPY --from=build --chown=nonroot:nonroot /go/src/fleet-telemetry/config/config.json /etc/fleet-telemetry/config.json

EXPOSE 443 8080

HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 CMD ["/healthcheck"]

ENTRYPOINT ["/fleet-telemetry"]
CMD ["-config=/etc/fleet-telemetry/config.json"]
