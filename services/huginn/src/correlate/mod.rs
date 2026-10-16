//! Correlação de eventos. Primeiro passo: deduplicar IOCs repetidos no mesmo
//! evento. Correlação entre eventos (janela temporal, agrupamento por IOC) é
//! trabalho futuro.

use crate::ingest::NormalizedEvent;
use std::collections::HashSet;

/// Aplica a correlação ao evento antes da publicação.
pub fn correlate(event: &mut NormalizedEvent) {
    // O mesmo indicador costuma aparecer várias vezes num log (header e
    // extensão CEF, por exemplo). Mantém a primeira ocorrência e a ordem.
    let mut seen = HashSet::new();
    event
        .iocs
        .retain(|ioc| seen.insert((ioc.kind, ioc.value.clone())));
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::ingest::{handle_payload, ContractIocKind};
    use std::collections::HashMap;

    fn event(raw: &str) -> NormalizedEvent {
        let map = HashMap::from([("raw".to_string(), raw.to_string())]);
        handle_payload(&map, "events:ingest:raw", "1-0")
    }

    #[test]
    fn dedups_repeated_iocs_keeping_first_occurrence_order() {
        let mut e = event("src=10.0.0.1 dst=evil.net src=10.0.0.1 x=EVIL.NET y=10.0.0.2");
        correlate(&mut e);
        let got: Vec<(ContractIocKind, &str)> =
            e.iocs.iter().map(|i| (i.kind, i.value.as_str())).collect();
        assert_eq!(
            got,
            vec![
                (ContractIocKind::Ip, "10.0.0.1"),
                (ContractIocKind::Domain, "evil.net"),
                (ContractIocKind::Ip, "10.0.0.2"),
            ]
        );
    }

    #[test]
    fn keeps_events_without_duplicates_untouched() {
        let mut e = event("src=10.0.0.1 dst=evil.net");
        let before = serde_json::to_value(&e).unwrap();
        correlate(&mut e);
        assert_eq!(serde_json::to_value(&e).unwrap(), before);
    }
}
