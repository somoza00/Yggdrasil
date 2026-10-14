//! Consumo do Redis Stream de ingest. Extrai eventos, aplica parsing,
//! enriquece (extrai IOCs) e publica o resultado para consumo da camada de IA.
//!
//! Estrutura do payload de entrada (campos do stream, JSON):
//! `raw` (o log original) e `source` ("cef" | "syslog" | ...). O campo opcional
//! `id` é o identificador do evento na fonte.

use crate::enrich;
use crate::parse;
use log::{info, warn};
use redis::aio::MultiplexedConnection;
use redis::streams::{StreamId, StreamReadOptions, StreamReadReply};
use redis::{AsyncCommands, RedisResult};
use serde::Serialize;
use std::collections::HashMap;
use std::time::Duration;

/// Espera após erro de leitura do Redis — evita loop quente se o Redis cair.
const READ_ERROR_BACKOFF: Duration = Duration::from_secs(1);

/// Quanto o XREADGROUP fica bloqueado no servidor esperando mensagens.
pub const READ_BLOCK: Duration = Duration::from_secs(5);

/// Timeout de resposta da conexão: precisa exceder o BLOCK. Com o default do
/// redis-rs (500ms) o cliente desiste antes do servidor responder, e uma
/// entrega tardia fica presa no PEL sem processamento.
pub const RESPONSE_TIMEOUT: Duration = Duration::from_secs(7);

/// Evento normalizado saído do pipeline (entrada para a camada de IA).
#[derive(Debug, Serialize)]
struct Event {
    id: Option<String>,
    source: String,
    raw: String,
    /// Resultado do parsing CEF/syslog (best-effort; `null_fields` se falhar).
    parsed: serde_json::Value,
    iocs: Vec<enrich::Ioc>,
}

/// Converte os campos de uma entrada do stream num mapa de campo->valor.
/// Campos cujo valor não é string são ignorados.
fn fields_to_map(entry: &StreamId) -> HashMap<String, String> {
    entry
        .map
        .iter()
        .filter_map(|(k, v)| {
            redis::from_redis_value_ref::<String>(v)
                .ok()
                .map(|s| (k.clone(), s))
        })
        .collect()
}

/// Roteia um payload para o parsing correto conforme a fonte.
fn parse_by_source(source: &str, raw: &str) -> serde_json::Value {
    match source {
        "cef" => match parse::parse_cef(raw) {
            Ok(e) => {
                serde_json::to_value(e).unwrap_or(serde_json::json!({"kind": "cef", "raw": raw}))
            }
            Err(err) => serde_json::json!({"kind": "cef", "error": err, "raw": raw}),
        },
        source if source.starts_with("syslog") => match parse::parse_syslog(raw) {
            Ok(e) => {
                serde_json::to_value(e).unwrap_or(serde_json::json!({"kind": "syslog", "raw": raw}))
            }
            Err(err) => serde_json::json!({"kind": "syslog", "error": err, "raw": raw}),
        },
        _ => serde_json::json!({"kind": "unknown", "raw": raw}),
    }
}

/// Processa um evento do stream. Nunca falha "para fora" — em erro de parsing
/// produz um evento degradado (não derruba nem venena a fila).
fn handle_payload(map: &HashMap<String, String>) -> Event {
    let raw = map.get("raw").cloned().unwrap_or_default();
    let source = map
        .get("source")
        .cloned()
        .unwrap_or_else(|| "unknown".into());
    let id = map.get("id").cloned();
    Event {
        id,
        source: source.clone(),
        raw: raw.clone(),
        parsed: parse_by_source(&source, &raw),
        iocs: enrich::extract_iocs(&raw),
    }
}

/// Loop principal de consumo. Lê o stream em grupo e faz ACK por evento,
/// somente depois que o evento normalizado foi publicado.
pub async fn run(
    mut c: MultiplexedConnection,
    stream: &str,
    group: &str,
    consumer: &str,
) -> Result<(), Box<dyn std::error::Error>> {
    // Cria o grupo (e o stream, via MKSTREAM) se não existir. Sem MKSTREAM o
    // grupo não é criado num stream inexistente e o XREADGROUP falha sem
    // bloquear. "BUSYGROUP" = grupo já existe (2ª execução em diante).
    let created: RedisResult<()> = c.xgroup_create_mkstream(stream, group, "$").await;
    match created {
        Ok(()) => info!("grupo criado stream={stream} group={group}"),
        Err(e) if e.code() == Some("BUSYGROUP") => {}
        Err(e) => return Err(e.into()),
    }

    let opts = StreamReadOptions::default()
        .group(group, consumer)
        .block(READ_BLOCK.as_millis() as usize);
    let out_stream = format!("{stream}:normalized");

    loop {
        // Timeout do BLOCK sem mensagens chega como resposta vazia (não erro).
        let reply: StreamReadReply = match c.xread_options(&[stream], &[">"], &opts).await {
            Ok(reply) => reply,
            Err(e) => {
                warn!("falha no XREADGROUP stream={stream}: {e}");
                tokio::time::sleep(READ_ERROR_BACKOFF).await;
                continue;
            }
        };

        for entry in reply.keys.into_iter().flat_map(|k| k.ids) {
            let event = handle_payload(&fields_to_map(&entry));
            info!(
                "evento id={} source={} iocs={}",
                event.id.as_deref().unwrap_or(&entry.id),
                event.source,
                event.iocs.len()
            );
            let payload = match serde_json::to_string(&event) {
                Ok(p) => p,
                Err(e) => {
                    warn!("falha ao serializar evento {}: {e}", entry.id);
                    continue;
                }
            };
            // Publica o resultado no stream de saída (consumo da camada de IA).
            // Em falha NÃO faz ACK: o evento fica no PEL para reprocessamento.
            let published: RedisResult<String> =
                c.xadd(&out_stream, "*", &[("event", payload)]).await;
            if let Err(e) = published {
                warn!(
                    "falha no XADD de {} em {out_stream}, sem ACK: {e}",
                    entry.id
                );
                continue;
            }
            let acked: RedisResult<i64> = c.xack(stream, group, &[&entry.id]).await;
            if let Err(e) = acked {
                warn!("falha no XACK de {}: {e}", entry.id);
            }
        }
    }
}
