//! Yggdrasil — huginn
//! Ingest de eventos de segurança: consome Redis Streams, faz parsing
//! CEF/syslog, enriquece IOCs e correlaciona. Saída publicada de volta
//! ao stream para a camada de IA (muninn).

mod ingest;
mod parse;
mod enrich;

use std::env;
use std::sync::Arc;

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    env_logger::init();

    let redis_url = env::var("REDIS_URL").unwrap_or_else(|_| "redis://127.0.0.1:6379".into());
    let stream = env::var("REDIS_STREAM_INGEST")
        .unwrap_or_else(|_| "events:ingest".into());
    let group = env::var("REDIS_CONSUMER_GROUP").unwrap_or_else(|_| "huginn".into());

    let mut client = redis::Client::open(redis_url.as_str())?;
    let pool = Arc::new(client.get_connection()?);

    log::info!("huginn consumindo stream={stream} group={group}");
    ingest::run(pool, &stream, &group).await?;
    Ok(())
}