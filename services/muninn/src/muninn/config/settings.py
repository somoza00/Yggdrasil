"""Configuração do muninn via pydantic-settings (variáveis de ambiente)."""

from pydantic_settings import BaseSettings, SettingsConfigDict


class Settings(BaseSettings):
    """Config central do muninn. Valores sobrescritos por env (pref MUNNIN_)."""

    model_config = SettingsConfigDict(env_prefix="MUNNIN_", env_file=".env", extra="ignore")

    port: int = 8090
    # Gateway LLM (LiteLLM)
    litellm_base_url: str = "http://127.0.0.1:4000"
    litellm_api_key: str = "sk-local"
    model: str = "gpt-4o-mini"
    # Observabilidade
    langfuse_host: str = "http://127.0.0.1:4000"
    langfuse_public_key: str = ""
    langfuse_secret_key: str = ""
    # Backend de orquestração (ygg-api)
    ygg_api_url: str = "http://127.0.0.1:8080"


def get_settings() -> Settings:
    return Settings()
