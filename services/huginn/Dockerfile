# Build estágio
FROM rust:1.82-alpine AS build
RUN apk add --no-cache musl-dev
WORKDIR /src
COPY Cargo.toml Cargo.lock* ./
COPY src ./src
RUN cargo build --release --locked 2>/dev/null \
    || cargo build --release

# Runtime distroless (estático musl)
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /src/target/release/huginn /usr/local/bin/huginn
ENTRYPOINT ["/usr/local/bin/huginn"]