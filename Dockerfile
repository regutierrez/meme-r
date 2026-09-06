# syntax=docker/dockerfile:1
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY *.go ./
COPY static/ ./static/
RUN CGO_ENABLED=0 go test ./...
ARG TARGETOS
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w" -o /out/memer-app .
RUN mkdir -p /runtime/data /runtime/tmp \
    && chown 10001:10001 /runtime/data \
    && chmod 1777 /runtime/tmp

FROM scratch
LABEL org.opencontainers.image.source="https://github.com/regutierrez/meme-r"
LABEL org.opencontainers.image.description="A small, single-user meme store with original GIF support."
COPY --from=build /out/memer-app /memer-app
COPY --from=build /runtime/ /
USER 10001:10001
WORKDIR /data
EXPOSE 6942
ENTRYPOINT ["/memer-app"]
CMD ["-addr", ":6942", "-data-dir", "/data"]
