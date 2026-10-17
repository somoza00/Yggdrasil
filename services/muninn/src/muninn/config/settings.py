"""Configuração do muninn via pydantic-settings (variáveis de ambiente)."""

import os

from pydantic import AliasChoices, Field, SecretStr
from pydantic_settings import BaseSettings, SettingsConfigDict


def _default_consumer_name() -> str:
    """Nome único por instância no consumer group. Em K8s/Docker, HOSTNAME é o
    nome do pod/container; um nome fixo faria réplicas dividirem o mesmo PEL."""
    return os.environ.get("HOSTNAME") or f"muninn-{os.getpid()}"


class Settings(BaseSettings):
    """Config central do muninn. Valores sobrescritos por env (pref MUNNIN_)."""

    model_config = SettingsConfigDict(env_prefix="MUNNIN_", env_file=".env", extra="ignore")

    port: int = 8090
    log_level: str = "INFO"
    # Gateway LLM (LiteLLM proxy, API compatível com OpenAI). Vazio desliga o
    # LLM: a triagem usa só o classificador heurístico.
    litellm_base_url: str = "http://127.0.0.1:4000"
    litellm_api_key: str = "sk-local"
    model: str = "gpt-4o-mini"
    llm_timeout_seconds: float = 30.0
    # Observabilidade
    langfuse_host: str = "http://127.0.0.1:4000"
    langfuse_public_key: str = ""
    langfuse_secret_key: str = ""
    # Backend de orquestração (ygg-api)
    ygg_api_url: str = "http://127.0.0.1:8080"
    ygg_api_timeout_seconds: float = 10.0
    # JWT de serviço enviado como Bearer ao ygg-api (emitido com o
    # cmd/ygg-token do ygg-api). Nome sem o prefixo MUNNIN_ de propósito: é
    # um segredo compartilhado com o ygg-api.
    ygg_api_service_token: SecretStr = Field(
        default=SecretStr(""),
        validation_alias=AliasChoices("YGG_API_SERVICE_TOKEN", "ygg_api_service_token"),
    )
    # Worker do stream de eventos normalizados (ADR-005)
    worker_enabled: bool = True
    redis_url: str = "redis://127.0.0.1:6379"
    redis_stream_normalized: str = "events:ingest:normalized"
    consumer_group: str = "muninn"
    consumer_name: str = Field(default_factory=_default_consumer_name)


def get_settings() -> Settings:
    return Settings()
