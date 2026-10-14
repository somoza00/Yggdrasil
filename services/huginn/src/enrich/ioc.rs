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
        let kind = if ip.is_ipv4() {
            IocKind::Ipv4
        } else {
            IocKind::Ipv6
        };
        return Some(Ioc {
            kind,
            value: t.to_string(),
        });
    }
    if let Some(kind) = detect_hash(t) {
        return Some(Ioc {
            kind,
            value: t.to_lowercase(),
        });
    }
    if looks_like_email(t) {
        return Some(Ioc {
            kind: IocKind::Email,
            value: t.to_lowercase(),
        });
    }
    // domínio simples (ex.: evil.example.com)
    if looks_like_domain(t) {
        return Some(Ioc {
            kind: IocKind::Domain,
            value: t.to_lowercase(),
        });
    }
    None
}

/// Extensões de arquivo que nunca são TLD real — evita `cmd.exe` como domínio.
const FILE_EXTENSIONS: &[&str] = &[
    "exe", "dll", "sys", "bat", "cmd", "vbs", "scr", "msi", "hta", "lnk", "tmp", "log", "txt",
    "dat", "ini", "cfg", "jar",
];

fn looks_like_domain(t: &str) -> bool {
    let parts: Vec<&str> = t.split('.').collect();
    if parts.len() < 2 {
        return false;
    }
    // TLD alfabético (descarta versões como `1.0`) e que não seja extensão de arquivo.
    let tld = parts[parts.len() - 1].to_ascii_lowercase();
    if tld.len() < 2
        || !tld.chars().all(|c| c.is_ascii_alphabetic())
        || FILE_EXTENSIONS.contains(&tld.as_str())
    {
        return false;
    }
    parts
        .iter()
        .all(|p| !p.is_empty() && p.chars().all(|c| c.is_ascii_alphanumeric() || c == '-'))
}

/// E-mail simples: exatamente um '@', parte local não vazia e domínio válido.
fn looks_like_email(t: &str) -> bool {
    match t.split_once('@') {
        Some((local, domain)) => {
            !local.is_empty()
                && local
                    .chars()
                    .all(|c| c.is_ascii_alphanumeric() || "._%+-".contains(c))
                && looks_like_domain(domain)
        }
        None => false,
    }
}

/// Extrai todos os IOCs detectáveis de um texto. Tokens separados por
/// espaços, vírgula, aspas, '=' (pares chave=valor) e '|' (header CEF).
pub fn extract_iocs(text: &str) -> Vec<Ioc> {
    text.split(|c: char| c.is_whitespace() || matches!(c, ',' | '"' | '\'' | '=' | '|'))
        .filter_map(classify)
        .collect()
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn classifies_ipv4_and_domain() {
        assert_eq!(
            classify("192.168.1.10").map(|i| i.kind),
            Some(IocKind::Ipv4)
        );
        assert_eq!(
            classify("evil.example.com").map(|i| i.kind),
            Some(IocKind::Domain)
        );
    }

    #[test]
    fn rejects_filenames_and_versions_as_domains() {
        assert_eq!(classify("cmd.exe"), None);
        assert_eq!(classify("payload.dll"), None);
        assert_eq!(classify("1.0"), None);
        assert_eq!(classify("2.4.1"), None);
        assert_eq!(classify("evil.co").map(|i| i.kind), Some(IocKind::Domain));
    }

    #[test]
    fn extracts_iocs_from_cef_header() {
        let iocs = extract_iocs("CEF:0|EDR|Agent|1.0|100|Beacon to c2.evil.net|9|src=10.0.0.5");
        assert!(iocs.iter().any(|i| i.value == "c2.evil.net"));
        assert!(iocs.iter().any(|i| i.value == "10.0.0.5"));
        assert!(!iocs.iter().any(|i| i.value == "1.0"));
    }

    #[test]
    fn classifies_email() {
        let ioc = classify("Attacker@Evil.example.com").unwrap();
        assert_eq!(ioc.kind, IocKind::Email);
        assert_eq!(ioc.value, "attacker@evil.example.com");
        assert_eq!(classify("@evil.com"), None);
        assert_eq!(classify("a@b@evil.com"), None);
        assert_eq!(classify("user@localhost"), None);
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
