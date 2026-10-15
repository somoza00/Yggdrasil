# Contratos entre serviços da Yggdrasil.
#
# events/ — contrato ASSÍNCRONO via Redis Streams (ADR-005, aceito):
#   normalized_event.schema.json  JSON Schema do NormalizedEvent 1.x
#   fixtures/                     saída real do huginn; validada pelos testes de
#                                 contrato do huginn (golden) e do muninn
#
# Protos gRPC (contrato SÍNCRONO): ainda não existem. Serão adicionados quando o
# ADR-002 (gRPC vs REST) decidir gRPC — aí entra o contrato entre ygg-api,
# huginn e muninn (buf + protoc). Se o spike decidir REST (OpenAPI), o contrato
# síncrono passa a viver nos schemas OpenAPI de muninn/ygg-api.
