# Build estágio
FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
COPY migrations ./migrations
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/ygg-api ./cmd/api \
 && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/ygg-token ./cmd/ygg-token

# Runtime mínimo
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/ygg-api /usr/local/bin/ygg-api
# Emissor de JWT de serviço (ex.: kubectl exec deploy/ygg-api -- ygg-token -sub muninn)
COPY --from=build /out/ygg-token /usr/local/bin/ygg-token
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/ygg-api"]