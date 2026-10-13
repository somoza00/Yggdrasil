"""Camada de observabilidade com Langfuse. Fica ativa quando creds presentes;
senão vira no-op para não quebrar dev/testes."""

from __future__ import annotations

from functools import lru_cache
from typing import Any

from muninn.config.settings import Settings


@lru_cache
def _langfuse_client(settings: Settings | None = None) -> Any:
    s = settings or Settings()
    if not s.langfuse_public_key or not s.langfuse_secret_key:
        return None
    from langfuse import Langfuse  # import tardio: só exige o pacote se usado

    return Langfuse(
        host=s.langfuse_host,
        public_key=s.langfuse_public_key,
        secret_key=s.langfuse_secret_key,
    )


def get_langfuse(settings: Settings | None = None) -> Any:
    """Retorna o cliente Langfuse, ou None se não configurado (no-op)."""
    return _langfuse_client(settings)