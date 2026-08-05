# Initful container image: single static binary as PID 1.
FROM golang:1.26-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /the-go-os ./cmd/the-go-os

FROM gcr.io/distroless/static-debian12
COPY --from=build /the-go-os /the-go-os
# Symlinks for multicall (distroless has no shell to create them at build — bake in build stage)
COPY --from=build /the-go-os /bin/init
COPY --from=build /the-go-os /bin/systemctl
COPY --from=build /the-go-os /bin/sh
COPY --from=build /the-go-os /bin/ls
COPY --from=build /the-go-os /bin/echo
COPY --from=build /the-go-os /bin/cat
ENTRYPOINT ["/the-go-os", "init"]
