# Initful container image: one multicall binary + symlinks it plants itself.
FROM golang:1.27-bookworm@sha256:d9b6a1beb9474adc2a1ce842573639b44b2a0dd49b6669f51d3821baf9ec37c9 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /the-go-os ./cmd/the-go-os

# Build a rootfs using our own ln (no host ln required in the final image).
RUN mkdir -p /rootfs/bin \
	&& cp /the-go-os /rootfs/the-go-os \
	&& for n in $(/the-go-os list-symlinks); do \
		/the-go-os ln -sfn /the-go-os /rootfs/bin/"$n"; \
	done \
	&& /the-go-os ln -sfn /the-go-os /rootfs/bin/the-go-os \
	&& /the-go-os ls -la /rootfs/bin | head

FROM gcr.io/distroless/static-debian12
COPY --from=build /rootfs/ /
ENV THE_GO_OS_CONTROL_SOCKET=/run/the-go-os/control.sock
# Prefer multicall names on PATH
ENV PATH=/bin
ENTRYPOINT ["/the-go-os", "init"]
