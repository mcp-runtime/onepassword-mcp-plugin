# Build from the repository root:
#   docker build --build-arg VERSION=0.1.0 -t onepassword-mcp-plugin .
# The image contains no 1Password token, no mappings and no keys. Mount them at run time.
FROM golang:1.26 AS build
ARG VERSION=dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" \
    -o /out/onepassword-mcp-plugin ./cmd/onepassword-mcp-plugin

FROM gcr.io/distroless/static-debian12:nonroot
ARG VERSION=dev
LABEL org.opencontainers.image.title="onepassword-mcp-plugin" \
      org.opencontainers.image.description="Per-user credential injection gateway for MCP servers, authenticated through mcp-auth" \
      org.opencontainers.image.source="https://github.com/mcp-runtime/onepassword-mcp-plugin" \
      org.opencontainers.image.version="${VERSION}"
COPY --from=build /out/onepassword-mcp-plugin /onepassword-mcp-plugin
USER nonroot:nonroot
EXPOSE 8090
ENV ONEPASSWORD_MCP_CONFIG=/etc/onepassword-mcp/config.json
ENTRYPOINT ["/onepassword-mcp-plugin"]
