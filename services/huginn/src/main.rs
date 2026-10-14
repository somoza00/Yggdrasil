//! Yggdrasil — huginn
//! Ingest de eventos de segurança: consome Redis Streams, faz parsing
//! CEF/syslog, enriquece IOCs e correlaciona. Saída publicada de volta
//! ao stream para a camada de IA (muninn).

mod enrich;
mod ingest;
mod parse;

use std::env;
use std::time::{SystemTime, UNIX_EPOCH};

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    env_logger::init();

    let redis_url = env::var("REDIS_URL").unwrap_or_else(|_| "redis://127.0.0.1:6379".into());
    let stream = env::var("REDIS_STREAM_INGEST").unwrap_or_else(|_| "events:ingest".into());
    let group = env::var("REDIS_CONSUMER_GROUP").unwrap_or_else(|_| "huginn".into());
    let consumer = consumer_name();

    let client = redis::Client::open(redis_url.as_str())?;
    let config =
        redis::AsyncConnectionConfig::new().set_response_timeout(Some(ingest::RESPONSE_TIMEOUT));
    let conn = client
        .get_multiplexed_async_connection_with_config(&config)
        .await?;

    log::info!("huginn consumindo stream={stream} group={group} consumer={consumer}");
    ingest::run(conn, &stream, &group, &consumer).await
}

/// Nome único do consumer por instância. Um nome fixo faria réplicas
/// compartilharem o mesmo PEL. Em K8s/Docker, HOSTNAME é o nome do pod.
fn consumer_name() -> String {
    env::var("HUGINN_CONSUMER_NAME")
        .or_else(|_| env::var("HOSTNAME"))
        .unwrap_or_else(|_| {
            let nanos = SystemTime::now()
                .duration_since(UNIX_EPOCH)
                .map(|d| d.as_nanos())
                .unwrap_or_default();
            format!("huginn-{}-{nanos}", std::process::id())
        })
}
