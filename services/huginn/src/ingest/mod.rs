//! Consumo do Redis Stream de ingest. Extrai eventos, aplica parsing,
//! enriquece (extrai IOCs) e publica o resultado para consumo da camada de IA.
//!
//! Estrutura do payload de entrada (campos do stream, JSON):
//! `raw` (o log original) e `source` ("cef" | "syslog" | ...). O campo opcional
//! `id` é o identificador do evento na fonte.

use crate::enrich;
use crate::parse;
use log::{info, warn};
use redis::aio::Connection;
use redis::AsyncCommands;
use serde::Serialize;
use std::collections::HashMap;
use std::sync::Arc;

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

/// Converte o stream (custódia `fields`) num mapa de campo->valor.
fn fields_to_map(fields: &[(String, String)]) -> HashMap<String, String> {
    fields
        .iter()
        .filter_map(|(k, v)| {
            let bytes = k.clone().into_bytes();
            let key = String::from_utf8_lossy(&bytes).to_string();
            Some((key, v.clone()))
        })
        .collect()
}

/// Roteia um payload para o parsing correto conforme a fonte.
fn parse_by_source(source: &str, raw: &str) -> serde_json::Value {
    match source {
        "cef" => match parse::parse_cef(raw) {
            Ok(e) => serde_json::to_value(e).unwrap_or(serde_json::json!({"kind": "cef", "raw": raw})),
            Err(err) => serde_json::json!({"kind": "cef", "error": err, "raw": raw}),
        },
        source if source.starts_with("syslog") => match parse::parse_syslog(raw) {
            Ok(e) => serde_json::to_value(e).unwrap_or(serde_json::json!({"kind": "syslog", "raw": raw})),
            Err(err) => serde_json::json!({"kind": "syslog", "error": err, "raw": raw}),
        },
        _ => serde_json::json!({"kind": "unknown", "raw": raw}),
    }
}

/// Processa um evento do stream. Nunca falha "para fora" — em erro de parsing
/// produz um evento degradado (não derruba nem venena a fila).
fn handle_payload(fields: &[(String, String)]) -> Event {
    let map = fields_to_map(fields);
    let raw = map.get("raw").cloned().unwrap_or_default();
    let source = map.get("source").cloned().unwrap_or_else(|| "unknown".into());
    let id = map.get("id").cloned();
    Event {
        id,
        source: source.clone(),
        raw: raw.clone(),
        parsed: parse_by_source(&source, &raw),
        iocs: enrich::extract_iocs(&raw),
    }
}

/// Loop principal de consumo. Lê o stream em grupo e faz ACK por evento.
pub async fn run(conn: Arc<Connection>, stream: &str, group: &str) -> Result<(), Box<dyn std::error::Error>> {
    let mut c = (*conn).clone();

    // Cria o grupo se não existir (ignora "BUSYGROUP" gerado na 2ª execução).
    let _ = c.xgroup_create::<_, _, redis::Value>(stream, group, "$").await;

    loop {
        let entries: Vec<(String, Vec<(String, String)>)> = c
            .xread_options(
                &[stream],
                &[">"],
                &redis::StreamReadOptions::default()
                    .group(group, "huginn-worker")
                    .block(5000),
            )
            .await
            // Redis retorna Value::Nil quando não há mensagem no timeout — OK.
            .unwrap_or_default();

        for (entry_id, fields) in entries {
            let event = handle_payload(&fields);
            info!(
                "evento id={} source={} iocs={}",
                event.id.as_deref().unwrap_or(&entry_id),
                event.source,
                event.iocs.len()
            );
            // Publica o resultado no stream de saída (consumo da camada de IA).
            let out_stream = format!("{stream}:normalized");
            let _: redis::Value = c
                .xadd(&out_stream, "*", &[("event", serde_json::to_string(&event).unwrap_or_default())])
                .await
                .unwrap_or(redis::Value::Nil);
            // ACK para não reentregar este evento.
            let _: i64 = c.xack(stream, group, &[entry_id.as_str()]).await.unwrap_or(0);
        }
    }
}