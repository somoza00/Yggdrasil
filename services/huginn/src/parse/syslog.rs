//! Parser simples de syslog (RFC 3164: `<PRI>Mmm dd hh:mm:ss hostname tag: msg`)
//! e de mensagem RFC 5424 básica.

/// Evento syslog normalizado (campos relevantes para a triagem).
#[derive(Debug, Clone, PartialEq, serde::Serialize)]
pub struct SyslogEvent {
    pub facility: u8,
    pub severity: u8,
    pub timestamp: Option<String>,
    pub hostname: Option<String>,
    pub message: String,
}

/// Decodifica o prefixo `<PRI>` (facility*8 + severity).
pub fn parse_priority(s: &str) -> Option<(u8, u8)> {
    let s = s.strip_prefix('<')?;
    let end = s.find('>')?;
    let pri: u8 = s[..end].parse().ok()?;
    Some((pri / 8, pri % 8))
}

/// Parsing pragmático de syslog. Concentra-se em extrair PRI, hostname e mensagem.
pub fn parse_syslog(line: &str) -> Result<SyslogEvent, String> {
    let (facility, severity) = parse_priority(line).ok_or("sem prefixo <PRI>")?;
    let rest = &line[line.find('>').map(|i| i + 1).unwrap_or(line.len())..];
    let rest = rest.trim_start();

    // RFC3164: "Mmm dd hh:mm:ss hostname tag[pid]: message"
    // heuristic: o hostname é tipicamente o 4º token de uma linha com timestamp.
    let mut tok = rest.splitn(5, ' ');
    let _ts = tok.next();
    let _t2 = tok.next();
    let _t3 = tok.next();
    let hostname = tok.next().map(|s| s.to_string()).filter(|s| !s.is_empty());
    let message = tok.next().unwrap_or("").to_string();

    Ok(SyslogEvent {
        facility,
        severity,
        timestamp: None, // estabilizar depois
        hostname,
        message,
    })
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn parses_priority() {
        // PRI 13 => facility 1 (user), severity 5 (notice)
        let (fac, sev) = parse_priority("<13>").unwrap();
        assert_eq!(fac, 1);
        assert_eq!(sev, 5);
    }

    #[test]
    fn parses_syslog_line() {
        let line = "<34>Oct 11 22:14:15 jp-srv sshd[5021]: Failed password for root";
        let e = parse_syslog(line).unwrap();
        assert_eq!(e.facility, 4);
        assert_eq!(e.severity, 2);
        assert_eq!(e.hostname.as_deref(), Some("jp-srv"));
        assert!(e.message.contains("Failed password"));
    }

    #[test]
    fn no_priority_is_error() {
        assert!(parse_syslog("sem pri aqui").is_err());
    }
}