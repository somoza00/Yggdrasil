# Build estágio
FROM python:3.13-slim AS build
ENV UV_COMPILE_BYTECODE=1 UV_LINK_MODE=copy UV_PYTHON_DOWNLOADS=never
WORKDIR /app
RUN pip install --no-cache-dir uv
# Dependências primeiro (camada cacheável), travadas pelo uv.lock
COPY pyproject.toml uv.lock README.md ./
RUN uv sync --frozen --no-dev --no-install-project
# Projeto instalado no venv como pacote (não editável): importável sem src/
COPY src ./src
RUN uv sync --frozen --no-dev --no-editable

# Runtime
FROM python:3.13-slim
RUN useradd --system --uid 10001 --no-create-home --shell /usr/sbin/nologin muninn
WORKDIR /app
COPY --from=build /app/.venv /app/.venv
ENV PATH="/app/.venv/bin:${PATH}"
USER 10001
EXPOSE 8090
CMD ["uvicorn", "muninn.app:app", "--host", "0.0.0.0", "--port", "8090"]
