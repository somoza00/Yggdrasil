"""Camada de observabilidade com Langfuse. Fica ativa quando creds presentes;
senão vira no-op para não quebrar dev/testes."""

from __future__ import annotations

from functools import cache
from typing import Any

from muninn.config.settings import Settings


def _build_client(s: Settings) -> Any:
    if not s.langfuse_public_key or not s.langfuse_secret_key:
        return None
    from langfuse import Langfuse  # import tardio: só exige o pacote se usado

    return Langfuse(
        host=s.langfuse_host,
        public_key=s.langfuse_public_key,
        secret_key=s.langfuse_secret_key,
    )


@cache
def _default_client() -> Any:
    """Cliente do ambiente, criado uma vez. Sem argumentos de propósito:
    Settings não é hashable e não pode ser chave de cache."""
    return _build_client(Settings())


def get_langfuse(settings: Settings | None = None) -> Any:
    """Retorna o cliente Langfuse, ou None se não configurado (no-op).
    Com `settings` explícito, cria um cliente novo (sem cache)."""
    if settings is None:
        return _default_client()
    return _build_client(settings)
