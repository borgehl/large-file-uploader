FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go vet ./... && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server
# Pre-create the data dirs owned by the runtime user so a fresh named volume inherits that ownership.
RUN mkdir -p /out/data/db /out/data/uploads && chown -R 10001:10001 /out/data

# Test target: docker build --target test ./backend
FROM build AS test
RUN go test ./...

FROM gcr.io/distroless/static-debian12:nonroot AS prod
COPY --from=build /out/server /server
COPY --from=build --chown=10001:10001 /out/data /data
USER 10001:10001
EXPOSE 8080
ENTRYPOINT ["/server"]
