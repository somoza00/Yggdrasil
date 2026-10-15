//! Consumo do Redis Stream de ingest. Extrai eventos, aplica parsing,
//! enriquece (extrai IOCs) e publica o resultado para consumo da camada de IA.
//!
//! Contrato definido no ADR-005 (`docs/adr/ADR-005-event-contract.md`).
//!
//! Entrada (`events:ingest:raw`, campos do stream): `raw` (o log original) e
//! `source` ("cef" | "syslog" | ...). O campo opcional `id` é o identificador
//! do evento na fonte (só vai para o log).
//!
//! Saída (`events:ingest:normalized`): campo `event` com o JSON do
//! `NormalizedEvent` (schema em `proto/events/normalized_event.schema.json`).

use crate::enrich::{self, IocKind};
use crate::parse;
use log::{info, warn};
use redis::aio::MultiplexedConnection;
use redis::streams::{StreamId, StreamReadOptions, StreamReadReply};
use redis::{AsyncCommands, RedisResult};
use serde::Serialize;
use std::collections::HashMap;
use std::time::Duration;
use uuid::Uuid;

/// Espera após erro de leitura do Redis — evita loop quente se o Redis cair.
const READ_ERROR_BACKOFF: Duration = Duration::from_secs(1);

/// Quanto o XREADGROUP fica bloqueado no servidor esperando mensagens.
pub const READ_BLOCK: Duration = Duration::from_secs(5);

/// Timeout de resposta da conexão: precisa exceder o BLOCK. Com o default do
/// redis-rs (500ms) o cliente desiste antes do servidor responder, e uma
/// entrega tardia fica presa no PEL sem processamento.
pub const RESPONSE_TIMEOUT: Duration = Duration::from_secs(7);

/// Versão do contrato emitida (ADR-005). Mudança aditiva = MINOR; quebra = MAJOR.
pub const SCHEMA_VERSION: &str = "1.0";

/// Namespace fixo do UUIDv5 do `event_id`. NUNCA alterar: mudaria o id de
/// eventos reentregues e quebraria a deduplicação nos consumidores.
const EVENT_ID_NAMESPACE: Uuid = uuid::uuid!("19e60a3f-8d45-44ed-8c3d-702817cc8016");

/// Limites do contrato (ver schema).
const TITLE_MAX_CHARS: usize = 256;
const SOURCE_MAX_CHARS: usize = 64;

/// Evento normalizado saído do pipeline (entrada para a camada de IA).
#[derive(Debug, Serialize)]
struct NormalizedEvent {
    schema_version: &'static str,
    /// Determinístico por entrada do stream raw: reentrega => mesmo id.
    event_id: Uuid,
    source: String,
    title: String,
    severity_hint: SeverityHint,
    raw: String,
    /// Resultado best-effort do parsing; sempre contém `kind`.
    parsed: serde_json::Value,
    iocs: Vec<EventIoc>,
}

/// Severidade sugerida pela fonte, normalizada. A triagem decide a final.
#[derive(Debug, Clone, Copy, PartialEq, Serialize)]
#[serde(rename_all = "lowercase")]
enum SeverityHint {
    Unknown,
    Info,
    Low,
    Medium,
    High,
    Critical,
}

/// Tipos de IOC do contrato 1.x. Mais grossos que `IocKind`: o detalhe
/// (IPv4/IPv6, SHA-256/MD5) é derivável do valor.
#[derive(Debug, Clone, Copy, PartialEq, Serialize)]
#[serde(rename_all = "lowercase")]
enum ContractIocKind {
    Ip,
    Domain,
    Hash,
    Url,
}

impl ContractIocKind {
    /// `None` = tipo detectado mas fora do contrato 1.0 (e-mail).
    fn from_kind(kind: IocKind) -> Option<Self> {
        match kind {
            IocKind::Ipv4 | IocKind::Ipv6 => Some(Self::Ip),
            IocKind::Domain => Some(Self::Domain),
            IocKind::Sha256 | IocKind::Sha1 | IocKind::Md5 => Some(Self::Hash),
            IocKind::Url => Some(Self::Url),
            IocKind::Email => None,
        }
    }
}

#[derive(Debug, Serialize)]
struct EventIoc {
    kind: ContractIocKind,
    value: String,
}

/// Resultado do parsing por fonte: o objeto `parsed` e o que dele se deriva.
struct ParseOutcome {
    parsed: serde_json::Value,
    title: Option<String>,
    severity: SeverityHint,
}

/// Severidade do header CEF: 0-10 ou Low/Medium/High/Very-High (spec CEF).
fn cef_severity(s: &str) -> SeverityHint {
    match s.trim().to_ascii_lowercase().as_str() {
        "low" => SeverityHint::Low,
        "medium" => SeverityHint::Medium,
        "high" => SeverityHint::High,
        "very-high" => SeverityHint::Critical,
        n => match n.parse::<u8>() {
            Ok(0..=3) => SeverityHint::Low,
            Ok(4..=6) => SeverityHint::Medium,
            Ok(7..=8) => SeverityHint::High,
            Ok(9..=10) => SeverityHint::Critical,
            _ => SeverityHint::Unknown,
        },
    }
}

/// Severidade syslog (RFC 5424): 0 emerg .. 7 debug.
fn syslog_severity(s: u8) -> SeverityHint {
    match s {
        0..=2 => SeverityHint::Critical,
        3 => SeverityHint::High,
        4 => SeverityHint::Medium,
        5 => SeverityHint::Low,
        _ => SeverityHint::Info,
    }
}

/// Serializa o resultado do parser e acrescenta `kind`.
fn parsed_with_kind<T: Serialize>(kind: &str, value: &T) -> serde_json::Value {
    let mut v = serde_json::to_value(value).unwrap_or_else(|_| serde_json::json!({}));
    if let Some(obj) = v.as_object_mut() {
        obj.insert("kind".into(), kind.into());
    }
    v
}

fn truncate_chars(s: &str, max: usize) -> String {
    s.chars().take(max).collect()
}

/// Título do evento: candidato do parser, senão 1ª linha não vazia do raw,
/// senão "<source> event". Nunca vazio; limitado a `TITLE_MAX_CHARS`.
fn make_title(candidate: Option<&str>, raw: &str, source: &str) -> String {
    let title = candidate
        .map(str::trim)
        .filter(|t| !t.is_empty())
        .or_else(|| raw.lines().map(str::trim).find(|l| !l.is_empty()))
        .map(str::to_string)
        .unwrap_or_else(|| format!("{source} event"));
    truncate_chars(&title, TITLE_MAX_CHARS)
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

/// Roteia um payload para o parsing correto conforme a fonte. O `raw` não é
/// repetido em `parsed`: já está no nível de cima do evento.
fn parse_by_source(source: &str, raw: &str) -> ParseOutcome {
    let failed = |kind: &str, err: String| ParseOutcome {
        parsed: serde_json::json!({"kind": kind, "error": err}),
        title: None,
        severity: SeverityHint::Unknown,
    };
    match source {
        "cef" => match parse::parse_cef(raw) {
            Ok(e) => ParseOutcome {
                parsed: parsed_with_kind("cef", &e),
                severity: cef_severity(&e.severity),
                title: Some(e.name),
            },
            Err(err) => failed("cef", err),
        },
        source if source.starts_with("syslog") => match parse::parse_syslog(raw) {
            Ok(e) => ParseOutcome {
                parsed: parsed_with_kind("syslog", &e),
                severity: syslog_severity(e.severity),
                title: Some(e.message),
            },
            Err(err) => failed("syslog", err),
        },
        _ => ParseOutcome {
            parsed: serde_json::json!({"kind": "unknown"}),
            title: None,
            severity: SeverityHint::Unknown,
        },
    }
}

/// Processa um evento do stream. Nunca falha "para fora" — em erro de parsing
/// produz um evento degradado (não derruba nem venena a fila).
///
/// `event_id` = UUIDv5(`<stream>/<entry_id>`): a reentrega da mesma entrada
/// (ex.: XADD ok mas XACK falhou) republica com o mesmo id.
fn handle_payload(map: &HashMap<String, String>, stream: &str, entry_id: &str) -> NormalizedEvent {
    let raw = map.get("raw").cloned().unwrap_or_default();
    let source = map
        .get("source")
        .map(|s| truncate_chars(s.trim(), SOURCE_MAX_CHARS))
        .filter(|s| !s.is_empty())
        .unwrap_or_else(|| "unknown".into());
    let outcome = parse_by_source(&source, &raw);
    let iocs = enrich::extract_iocs(&raw)
        .into_iter()
        .filter_map(|ioc| {
            ContractIocKind::from_kind(ioc.kind).map(|kind| EventIoc {
                kind,
                value: ioc.value,
            })
        })
        .collect();
    NormalizedEvent {
        schema_version: SCHEMA_VERSION,
        event_id: Uuid::new_v5(
            &EVENT_ID_NAMESPACE,
            format!("{stream}/{entry_id}").as_bytes(),
        ),
        title: make_title(outcome.title.as_deref(), &raw, &source),
        severity_hint: outcome.severity,
        parsed: outcome.parsed,
        source,
        raw,
        iocs,
    }
}

/// Loop principal de consumo. Lê o stream em grupo e faz ACK por evento,
/// somente depois que o evento normalizado foi publicado.
pub async fn run(
    mut c: MultiplexedConnection,
    stream: &str,
    out_stream: &str,
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
            let map = fields_to_map(&entry);
            let event = handle_payload(&map, stream, &entry.id);
            info!(
                "evento event_id={} entry={} source_id={} source={} severity={:?} iocs={}",
                event.event_id,
                entry.id,
                map.get("id").map_or("-", String::as_str),
                event.source,
                event.severity_hint,
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
                c.xadd(out_stream, "*", &[("event", payload)]).await;
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

#[cfg(test)]
mod tests {
    use super::*;
    use std::collections::BTreeSet;

    const RAW_STREAM: &str = "events:ingest:raw";
    const SCHEMA: &str = include_str!("../../../../proto/events/normalized_event.schema.json");
    const FIXTURE_CEF: &str = include_str!("../../../../proto/events/fixtures/valid_cef.json");
    const FIXTURE_SYSLOG: &str =
        include_str!("../../../../proto/events/fixtures/valid_syslog.json");
    const FIXTURE_MINIMAL: &str = include_str!("../../../../proto/events/fixtures/minimal.json");

    fn payload(fields: &[(&str, &str)]) -> HashMap<String, String> {
        fields
            .iter()
            .map(|(k, v)| (k.to_string(), v.to_string()))
            .collect()
    }

    fn to_json(event: &NormalizedEvent) -> serde_json::Value {
        serde_json::to_value(event).unwrap()
    }

    fn fixture_raw(fixture: &str) -> String {
        let v: serde_json::Value = serde_json::from_str(fixture).unwrap();
        v["raw"].as_str().unwrap().to_string()
    }

    fn schema() -> serde_json::Value {
        serde_json::from_str(SCHEMA).unwrap()
    }

    fn string_set(v: &serde_json::Value) -> BTreeSet<String> {
        v.as_array()
            .unwrap()
            .iter()
            .map(|s| s.as_str().unwrap().to_string())
            .collect()
    }

    // Golden tests: as fixtures em proto/events/fixtures são exatamente o que o
    // huginn produz. O muninn valida as mesmas fixtures → contrato ponta a ponta.

    #[test]
    fn cef_event_matches_fixture() {
        let raw = fixture_raw(FIXTURE_CEF);
        let map = payload(&[("source", "cef"), ("raw", &raw)]);
        let event = handle_payload(&map, RAW_STREAM, "1700000000000-0");
        let expected: serde_json::Value = serde_json::from_str(FIXTURE_CEF).unwrap();
        assert_eq!(to_json(&event), expected);
    }

    #[test]
    fn syslog_event_matches_fixture() {
        let raw = fixture_raw(FIXTURE_SYSLOG);
        let map = payload(&[("source", "syslog"), ("raw", &raw)]);
        let event = handle_payload(&map, RAW_STREAM, "1700000000000-1");
        let expected: serde_json::Value = serde_json::from_str(FIXTURE_SYSLOG).unwrap();
        assert_eq!(to_json(&event), expected);
    }

    #[test]
    fn minimal_event_matches_fixture() {
        let map = payload(&[("raw", "heartbeat")]);
        let event = handle_payload(&map, RAW_STREAM, "1700000000000-2");
        let expected: serde_json::Value = serde_json::from_str(FIXTURE_MINIMAL).unwrap();
        assert_eq!(to_json(&event), expected);
    }

    #[test]
    fn serialized_fields_match_schema_required() {
        let event = to_json(&handle_payload(
            &payload(&[("raw", "x")]),
            RAW_STREAM,
            "1-0",
        ));
        let keys: BTreeSet<String> = event.as_object().unwrap().keys().cloned().collect();
        assert_eq!(keys, string_set(&schema()["required"]));
    }

    #[test]
    fn enums_match_schema() {
        let schema = schema();
        let severities: BTreeSet<String> = [
            SeverityHint::Unknown,
            SeverityHint::Info,
            SeverityHint::Low,
            SeverityHint::Medium,
            SeverityHint::High,
            SeverityHint::Critical,
        ]
        .iter()
        .map(|s| {
            serde_json::to_value(s)
                .unwrap()
                .as_str()
                .unwrap()
                .to_string()
        })
        .collect();
        assert_eq!(
            severities,
            string_set(&schema["properties"]["severity_hint"]["enum"])
        );

        let kinds: BTreeSet<String> = [
            ContractIocKind::Ip,
            ContractIocKind::Domain,
            ContractIocKind::Hash,
            ContractIocKind::Url,
        ]
        .iter()
        .map(|k| {
            serde_json::to_value(k)
                .unwrap()
                .as_str()
                .unwrap()
                .to_string()
        })
        .collect();
        assert_eq!(
            kinds,
            string_set(&schema["properties"]["iocs"]["items"]["properties"]["kind"]["enum"])
        );
    }

    #[test]
    fn event_id_is_deterministic_per_stream_entry() {
        let map = payload(&[("raw", "x")]);
        let a = handle_payload(&map, RAW_STREAM, "1-0").event_id;
        let again = handle_payload(&map, RAW_STREAM, "1-0").event_id;
        let other = handle_payload(&map, RAW_STREAM, "1-1").event_id;
        assert_eq!(
            a, again,
            "reentrega da mesma entrada deve manter o event_id"
        );
        assert_ne!(a, other);
    }

    #[test]
    fn maps_cef_severity() {
        assert_eq!(cef_severity("0"), SeverityHint::Low);
        assert_eq!(cef_severity("3"), SeverityHint::Low);
        assert_eq!(cef_severity("4"), SeverityHint::Medium);
        assert_eq!(cef_severity("7"), SeverityHint::High);
        assert_eq!(cef_severity("10"), SeverityHint::Critical);
        assert_eq!(cef_severity("Very-High"), SeverityHint::Critical);
        assert_eq!(cef_severity("medium"), SeverityHint::Medium);
        assert_eq!(cef_severity("11"), SeverityHint::Unknown);
        assert_eq!(cef_severity("?"), SeverityHint::Unknown);
    }

    #[test]
    fn maps_syslog_severity() {
        assert_eq!(syslog_severity(0), SeverityHint::Critical);
        assert_eq!(syslog_severity(2), SeverityHint::Critical);
        assert_eq!(syslog_severity(3), SeverityHint::High);
        assert_eq!(syslog_severity(4), SeverityHint::Medium);
        assert_eq!(syslog_severity(5), SeverityHint::Low);
        assert_eq!(syslog_severity(7), SeverityHint::Info);
    }

    #[test]
    fn title_falls_back_and_is_bounded() {
        // CEF malformado: título vem da 1ª linha não vazia do raw.
        let map = payload(&[("source", "cef"), ("raw", "\n  not cef at all \nsecond")]);
        let event = handle_payload(&map, RAW_STREAM, "1-0");
        assert_eq!(event.title, "not cef at all");
        assert!(event.parsed["error"].is_string());
        assert_eq!(event.parsed["kind"], "cef");

        let empty = handle_payload(&payload(&[("source", "edr")]), RAW_STREAM, "1-0");
        assert_eq!(empty.title, "edr event");

        let long = "á".repeat(1000);
        let event = handle_payload(&payload(&[("raw", &long)]), RAW_STREAM, "1-0");
        assert_eq!(event.title.chars().count(), TITLE_MAX_CHARS);
    }

    #[test]
    fn blank_source_becomes_unknown() {
        let event = handle_payload(
            &payload(&[("source", "  "), ("raw", "x")]),
            RAW_STREAM,
            "1-0",
        );
        assert_eq!(event.source, "unknown");
    }

    #[test]
    fn email_iocs_are_not_emitted_in_v1() {
        let map = payload(&[("raw", "from attacker@evil.example.com to 10.0.0.1")]);
        let event = handle_payload(&map, RAW_STREAM, "1-0");
        assert_eq!(event.iocs.len(), 1);
        assert_eq!(event.iocs[0].kind, ContractIocKind::Ip);
    }
}
