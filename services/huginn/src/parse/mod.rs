//! Parsing de logs de segurança: CEF e syslog.

pub mod cef;
pub mod syslog;

pub use cef::parse_cef;
pub use syslog::parse_syslog;
