# Build estágio
FROM rust:1.90-alpine AS build
RUN apk add --no-cache musl-dev
WORKDIR /src
# Dependências travadas pelo Cargo.lock (build reprodutível; falha se divergir)
COPY Cargo.toml Cargo.lock ./
COPY src ./src
RUN cargo build --release --locked

# Runtime distroless (estático musl)
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /src/target/release/huginn /usr/local/bin/huginn
ENTRYPOINT ["/usr/local/bin/huginn"]
