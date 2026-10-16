"""E2E da primeira fatia vertical: um log CEF entra, um incidente sai.

    events:ingest:raw → huginn → events:ingest:normalized → muninn (triagem)
    → ygg-api POST /api/v1/incidents → GET /api/v1/incidents

Precisa da stack no ar (`docker compose up`). Roda no container one-shot do
compose (`make e2e`) ou do host, com os defaults abaixo.
"""

from __future__ import annotations

import json
import os
import time
import urllib.request
import uuid
from collections.abc import Iterator
from typing import Any

import pytest
import redis

REDIS_URL = os.environ.get("E2E_REDIS_URL", "redis://localhost:6379")
YGG_API_URL = os.environ.get("E2E_YGG_API_URL", "http://localhost:8080").rstrip("/")
RAW_STREAM = os.environ.get("E2E_RAW_STREAM", "events:ingest:raw")
NORMALIZED_STREAM = os.environ.get("E2E_NORMALIZED_STREAM", "events:ingest:normalized")
HUGINN_GROUP = os.environ.get("E2E_HUGINN_GROUP", "huginn")
MUNINN_GROUP = os.environ.get("E2E_MUNINN_GROUP", "muninn")

# Subida da stack (build, start) fica fora do orçamento do fluxo.
READY_TIMEOUT_S = float(os.environ.get("E2E_READY_TIMEOUT", "90"))
FLOW_TIMEOUT_S = 10.0


def get_json(path: str) -> Any:
    with urllib.request.urlopen(f"{YGG_API_URL}{path}", timeout=5) as resp:
        return json.load(resp)


def group_exists(r: redis.Redis, stream: str, group: str) -> bool:
    try:
        return any(g["name"] == group for g in r.xinfo_groups(stream))
    except redis.ResponseError:  # stream ainda não existe
        return False


def stack_ready(r: redis.Redis) -> bool:
    try:
        get_json("/healthz")
    except OSError:
        return False
    # Os grupos são criados com "$": um evento publicado antes disso seria
    # ignorado, então só segue quando huginn e muninn já estão consumindo.
    return group_exists(r, RAW_STREAM, HUGINN_GROUP) and group_exists(
        r, NORMALIZED_STREAM, MUNINN_GROUP
    )


@pytest.fixture(scope="module")
def r() -> Iterator[redis.Redis]:
    client = redis.Redis.from_url(REDIS_URL, decode_responses=True)
    deadline = time.monotonic() + READY_TIMEOUT_S
    while True:
        try:
            if stack_ready(client):
                break
        except redis.ConnectionError:
            pass
        if time.monotonic() > deadline:
            pytest.fail(f"stack não ficou pronta em {READY_TIMEOUT_S}s")
        time.sleep(0.5)
    yield client
    client.close()


def wait_for_incident(title: str) -> dict[str, Any]:
    deadline = time.monotonic() + FLOW_TIMEOUT_S
    while time.monotonic() < deadline:
        for incident in get_json("/api/v1/incidents"):
            if incident["title"] == title:
                return dict(incident)
        time.sleep(0.25)
    pytest.fail(f"nenhum incidente {title!r} em {FLOW_TIMEOUT_S}s")


def test_cef_log_becomes_incident(r: redis.Redis) -> None:
    nonce = uuid.uuid4().hex[:12]
    title = f"E2E beacon {nonce}"
    raw = (
        f"CEF:0|ACME|EDR Agent|2.1|1001|{title}|9|"
        f"src=10.0.0.5 request=http://c2.evil.net/{nonce} msg=powershell -enc AAAA"
    )
    r.xadd(RAW_STREAM, {"source": "cef", "raw": raw})

    incident = wait_for_incident(title)

    assert incident["severity"] == "critical"  # CEF 9 → critical (ADR-005)
    assert incident["category"]
    assert incident["engine"] in ("llm", "heuristic")
    assert incident["model"]
    assert incident["verdict"] in ("true_positive", "false_positive", "review")
    assert incident["status"] == "open"
    uuid.UUID(incident["event_id"])
    assert {"kind": "url", "value": f"http://c2.evil.net/{nonce}"} in incident["iocs"]
