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
    let (fields, ext) =
        split_header(line).ok_or_else(|| format!("CEF malformado (esperava 7 campos): {line}"))?;
    // fields[0] == "CEF:Version"
    let version = fields[0]
        .strip_prefix("CEF:")
        .ok_or("prefixo CEF: ausente")?
        .to_string();
    let [_, vendor, product, product_version, signature_id, name, severity] = fields;

    Ok(CefEvent {
        version,
        vendor,
        product,
        product_version,
        signature_id,
        name,
        severity,
        extension: parse_extension(ext),
    })
}

/// Separa os 7 campos do header (desescapando `\|` e `\\`) e devolve o resto
/// da linha — a extensão, ainda escapada.
fn split_header(line: &str) -> Option<([String; 7], &str)> {
    let mut fields: Vec<String> = Vec::with_capacity(7);
    let mut cur = String::new();
    let mut chars = line.char_indices();
    while let Some((i, c)) = chars.next() {
        match c {
            '\\' => match chars.next() {
                Some((_, n @ ('|' | '\\'))) => cur.push(n),
                Some((_, n)) => {
                    cur.push('\\');
                    cur.push(n);
                }
                None => cur.push('\\'),
            },
            '|' => {
                fields.push(std::mem::take(&mut cur));
                if fields.len() == 7 {
                    return Some((fields.try_into().ok()?, &line[i + 1..]));
                }
            }
            _ => cur.push(c),
        }
    }
    None
}

/// Parseia a extensão `k1=v1 k2=v2` conforme a spec CEF: valores podem conter
/// espaços e vão até o próximo ` chave=`; `\=`, `\\`, `\n` e `\r` são
/// desescapados. Por compatibilidade, aspas envolvendo o valor são removidas.
pub fn parse_extension(ext: &str) -> HashMap<String, String> {
    // (início da chave, posição do '=') de cada par válido.
    let mut keys: Vec<(usize, usize)> = Vec::new();
    let mut chars = ext.char_indices();
    while let Some((i, c)) = chars.next() {
        match c {
            '\\' => {
                chars.next(); // caractere escapado nunca separa chave/valor
            }
            '=' => {
                let start = ext[..i].rfind(' ').map_or(0, |s| s + 1);
                let after_prev = keys.last().is_none_or(|&(_, eq)| start > eq);
                if after_prev && is_valid_key(&ext[start..i]) {
                    keys.push((start, i));
                }
                // '=' sem chave válida (não escapado) fica como parte do valor.
            }
            _ => {}
        }
    }

    let mut out = HashMap::new();
    for (n, &(start, eq)) in keys.iter().enumerate() {
        let end = keys.get(n + 1).map_or(ext.len(), |&(next, _)| next);
        let raw = ext[eq + 1..end].trim_end();
        let raw = raw
            .strip_prefix('"')
            .and_then(|v| v.strip_suffix('"'))
            .unwrap_or(raw);
        out.insert(ext[start..eq].to_string(), unescape_value(raw));
    }
    out
}

fn is_valid_key(k: &str) -> bool {
    !k.is_empty()
        && k.chars()
            .all(|c| c.is_ascii_alphanumeric() || matches!(c, '_' | '.' | '-'))
}

fn unescape_value(v: &str) -> String {
    let mut out = String::with_capacity(v.len());
    let mut chars = v.chars();
    while let Some(c) = chars.next() {
        if c != '\\' {
            out.push(c);
            continue;
        }
        match chars.next() {
            Some('n') => out.push('\n'),
            Some('r') => out.push('\r'),
            Some(n @ ('=' | '\\')) => out.push(n),
            Some(n) => {
                out.push('\\');
                out.push(n);
            }
            None => out.push('\\'),
        }
    }
    out
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn parses_simple_cef() {
        let line = "CEF:0|EDR|Agent|1.0|20034|Suspicious Activity|7|src=192.168.1.10";
        let e = parse_cef(line).unwrap();
        assert_eq!(e.vendor, "EDR");
        assert_eq!(e.product, "Agent");
        assert_eq!(e.signature_id, "20034");
        assert_eq!(e.severity, "7");
        assert_eq!(
            e.extension.get("src").map(String::as_str),
            Some("192.168.1.10")
        );
    }

    #[test]
    fn rejects_malformed() {
        assert!(parse_cef("apenas texto").is_err());
    }

    #[test]
    fn handles_quoted_extension_value() {
        let line = r#"CEF:0|V|P|1|1|Name|3|request="/login" src=10.0.0.1"#;
        let e = parse_cef(line).unwrap();
        assert_eq!(
            e.extension.get("request").map(String::as_str),
            Some("/login")
        );
        assert_eq!(e.extension.get("src").map(String::as_str), Some("10.0.0.1"));
    }

    #[test]
    fn splits_multiple_unquoted_values() {
        let e = parse_cef("CEF:0|V|P|1|1|N|3|src=10.0.0.1 dst=10.0.0.2 act=blocked").unwrap();
        assert_eq!(e.extension.get("src").map(String::as_str), Some("10.0.0.1"));
        assert_eq!(e.extension.get("dst").map(String::as_str), Some("10.0.0.2"));
        assert_eq!(e.extension.get("act").map(String::as_str), Some("blocked"));
    }

    #[test]
    fn value_with_spaces_runs_until_next_key() {
        let e = parse_cef("CEF:0|V|P|1|1|N|3|msg=user root failed login src=10.0.0.1").unwrap();
        assert_eq!(
            e.extension.get("msg").map(String::as_str),
            Some("user root failed login")
        );
        assert_eq!(e.extension.get("src").map(String::as_str), Some("10.0.0.1"));
    }

    #[test]
    fn unescapes_extension_value() {
        let e = parse_cef(r"CEF:0|V|P|1|1|N|3|cs1=a\=b c\\d src=1.1.1.1").unwrap();
        assert_eq!(e.extension.get("cs1").map(String::as_str), Some(r"a=b c\d"));
        assert_eq!(e.extension.get("src").map(String::as_str), Some("1.1.1.1"));
    }

    #[test]
    fn handles_escaped_pipe_in_header() {
        let e = parse_cef(r"CEF:0|V|P\|X|1|1|N|3|src=1.1.1.1").unwrap();
        assert_eq!(e.product, "P|X");
        assert_eq!(e.severity, "3");
        assert_eq!(e.extension.get("src").map(String::as_str), Some("1.1.1.1"));
    }

    #[test]
    fn empty_extension_is_ok() {
        let e = parse_cef("CEF:0|V|P|1|1|N|3|").unwrap();
        assert!(e.extension.is_empty());
    }
}
