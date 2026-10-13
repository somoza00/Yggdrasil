//! Enriquecimento de IOCs (Indicators of Compromise): IP, domínio, hash,
//! e-mail. Detecta/harmoniza indicadores encontrados num evento.

use std::net::IpAddr;

/// Um indicador extraído/harmonizado de um evento.
#[derive(Debug, Clone, PartialEq, serde::Serialize)]
pub struct Ioc {
    pub kind: IocKind,
    pub value: String,
}

#[derive(Debug, Clone, Copy, PartialEq, serde::Serialize)]
pub enum IocKind {
    Ipv4,
    Ipv6,
    Domain,
    Sha256,
    Sha1,
    Md5,
    Email,
}

/// Detecta se `token` é um hash e de qual tipo.
pub fn detect_hash(token: &str) -> Option<IocKind> {
    if token.len() == 64 && token.chars().all(|c| c.is_ascii_hexdigit()) {
        Some(IocKind::Sha256)
    } else if token.len() == 40 && token.chars().all(|c| c.is_ascii_hexdigit()) {
        Some(IocKind::Sha1)
    } else if token.len() == 32 && token.chars().all(|c| c.is_ascii_hexdigit()) {
        Some(IocKind::Md5)
    } else {
        None
    }
}

/// Classifica um token bruto em um IOC, retornando None se não parecer um.
pub fn classify(token: &str) -> Option<Ioc> {
    let t = token.trim();
    if t.is_empty() {
        return None;
    }
    if let Ok(ip) = t.parse::<IpAddr>() {
        let kind = if ip.is_ipv4() { IocKind::Ipv4 } else { IocKind::Ipv6 };
        return Some(Ioc { kind, value: t.to_string() });
    }
    if let Some(kind) = detect_hash(t) {
        return Some(Ioc { kind, value: t.to_lowercase() });
    }
    // domínio simples (ex.: evil.example.com)
    if looks_like_domain(t) {
        return Some(Ioc { kind: IocKind::Domain, value: t.to_lowercase() });
    }
    None
}

fn looks_like_domain(t: &str) -> bool {
    let parts: Vec<&str> = t.split('.').collect();
    if parts.len() < 2 {
        return false;
    }
    parts
        .iter()
        .all(|p| !p.is_empty() && p.chars().all(|c| c.is_ascii_alphanumeric() || c == '-'))
}

/// Extrai todos os IOCs detectáveis de um texto (tokens separados por espaços/comma).
pub fn extract_iocs(text: &str) -> Vec<Ioc> {
    text.split(|c: char| c.is_whitespace() || c == ',' || c == '"' || c == '\'')
        .filter_map(classify)
        .collect()
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn classifies_ipv4_and_domain() {
        assert_eq!(classify("192.168.1.10").map(|i| i.kind), Some(IocKind::Ipv4));
        assert_eq!(classify("evil.example.com").map(|i| i.kind), Some(IocKind::Domain));
    }

    #[test]
    fn detects_hash_kinds() {
        let sha256 = "a".repeat(64);
        assert_eq!(detect_hash(&sha256), Some(IocKind::Sha256));
        let md5 = "b".repeat(32);
        assert_eq!(detect_hash(&md5), Some(IocKind::Md5));
    }

    #[test]
    fn extracts_multiple_iocs() {
        let iocs = extract_iocs("src=10.0.0.1 dst=malsite.xyz sha=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa");
        assert!(iocs.iter().any(|i| i.kind == IocKind::Ipv4));
        assert!(iocs.iter().any(|i| i.kind == IocKind::Domain));
        assert!(iocs.iter().any(|i| i.kind == IocKind::Sha256));
    }
}