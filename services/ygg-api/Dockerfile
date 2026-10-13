# Build estágio
FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/ygg-api ./cmd/api

# Runtime mínimo
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/ygg-api /usr/local/bin/ygg-api
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/ygg-api"]