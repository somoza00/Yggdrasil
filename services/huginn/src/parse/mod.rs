//! Parsing de logs de segurança: CEF e syslog.

pub mod cef;
pub mod syslog;

pub use cef::{CefEvent, parse_cef};
pub use syslog::{SyslogEvent, parse_syslog};