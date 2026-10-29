# Initful container image: one multicall binary + symlinks it plants itself.
FROM golang:1.26-bookworm AS build
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

FROM gcr.io/distroless/static-debian12@sha256:d75cdd72874d4790092fcb1b058493ecf6bb5bf2b2b897045b00ff01d91843f2
COPY --from=build /rootfs/ /
ENV THE_GO_OS_CONTROL_SOCKET=/run/the-go-os/control.sock
# Prefer multicall names on PATH
ENV PATH=/bin
ENTRYPOINT ["/the-go-os", "init"]
