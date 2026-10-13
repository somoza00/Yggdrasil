# Build estágio
FROM python:3.13-slim AS build
ENV UV_COMPILE_BYTECODE=1 UV_LINK_MODE=copy
WORKDIR /app
RUN pip install --no-cache-dir uv
COPY pyproject.toml ./
COPY README.md ./
COPY src ./src
RUN uv sync --no-dev --no-install-project

# Runtime
FROM python:3.13-slim
ENV PATH="/root/.local/bin:${PATH}"
WORKDIR /app
COPY --from=build /app/.venv /app/.venv
COPY src ./src
EXPOSE 8090
CMD ["/app/.venv/bin/uvicorn", "muninn.app:app", "--host", "0.0.0.0", "--port", "8090"]