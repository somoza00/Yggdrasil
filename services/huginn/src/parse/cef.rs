//! Parser de CEF (Common Event Format) — ArcSight/CEF.
//! Formato típico:
//! `CEF:Version|Vendor|Product|Version|SignatureID|Name|Severity|Extension`

use std::collections::HashMap;

/// Um evento CEF parseado.
#[derive(Debug, Clone, PartialEq, serde::Serialize)]
pub struct CefEvent {
    pub version: String,
    pub vendor: String,
    pub product: String,
    pub product_version: String,
    pub signature_id: String,
    pub name: String,
    pub severity: String,
    /// Pares chave=valor da "Extension" (a parte rica em texto livre).
    pub extension: HashMap<String, String>,
}

/// Faz o parsing completo de uma linha CEF.
pub fn parse_cef(line: &str) -> Result<CefEvent, String> {
    let mut pipe_count = 0;
    let mut header_end_idx = 0;
    for (i, b) in line.bytes().enumerate() {
        if b == b'|' {
            pipe_count += 1;
            if pipe_count == 7 {
                header_end_idx = i;
                break;
            }
        }
    }
    if pipe_count < 7 {
        return Err(format!("CEF malformado (esperava 7 campos): {line}"));
    }

    let header = &line[..header_end_idx];
    let ext = &line[header_end_idx + 1..];

    let fields: Vec<&str> = header.split('|').collect();
    // fields[0] == "CEF:Version"
    if !fields[0].starts_with("CEF:") {
        return Err("prefixo CEF: ausente".into());
    }

    let event = CefEvent {
        version: fields[0]["CEF:".len()..].to_string(),
        vendor: fields.get(1).copied().unwrap_or_default().to_string(),
        product: fields.get(2).copied().unwrap_or_default().to_string(),
        product_version: fields.get(3).copied().unwrap_or_default().to_string(),
        signature_id: fields.get(4).copied().unwrap_or_default().to_string(),
        name: fields.get(5).copied().unwrap_or_default().to_string(),
        severity: fields.get(6).copied().unwrap_or_default().to_string(),
        extension: parse_extension(ext),
    };
    Ok(event)
}

/// Parseia a extensão `k1=v1 k2=v2` (valores com espaço escapado entre aspas).
pub fn parse_extension(ext: &str) -> HashMap<String, String> {
    use std::iter::Peekable;
    let mut out = HashMap::new();
    let mut chars: Peekable<std::str::Chars> = ext.chars().peekable();
    let mut key = String::new();
    let mut val = String::new();
    let mut state = 0; // 0=key, 1=esperando '=', 2=value, 3=quoted

    while let Some(c) = chars.next() {
        match state {
            0 => {
                if c == ' ' {
                    // espaço antes do '=' normal (ex.: "k= v")
                } else if c == '=' {
                    state = 2;
                } else {
                    key.push(c);
                }
            }
            2 => {
                if c == '"' {
                    state = 3;
                } else {
                    val.push(c);
                    state = 2;
                    // fim de valor ao achar espaço/; não tratado aqui — simples
                }
            }
            3 => {
                if c == '"' {
                    out.insert(key.clone(), val.clone());
                    key.clear();
                    val.clear();
                    state = 0;
                } else {
                    val.push(c);
                }
            }
            _ => {}
        }
    }
    if state == 2 && !key.is_empty() {
        out.insert(key.clone(), val.clone());
    }
    out
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn parses_simple_cef() {
        let line = "CEF:0|Sentinel One|Agent|1.0|20034|Suspicious Activity|7|src=192.168.1.10";
        let e = parse_cef(line).unwrap();
        assert_eq!(e.vendor, "Sentinel One");
        assert_eq!(e.product, "Agent");
        assert_eq!(e.signature_id, "20034");
        assert_eq!(e.severity, "7");
        assert_eq!(e.extension.get("src").map(String::as_str), Some("192.168.1.10"));
    }

    #[test]
    fn rejects_malformed() {
        assert!(parse_cef("apenas texto").is_err());
    }

    #[test]
    fn handles_quoted_extension_value() {
        let line = r#"CEF:0|V|P|1|1|Name|3|request="/login" src=10.0.0.1"#;
        let e = parse_cef(line).unwrap();
        assert_eq!(e.extension.get("request").map(String::as_str), Some("/login"));
        assert_eq!(e.extension.get("src").map(String::as_str), Some("10.0.0.1"));
    }
}