# ADR-006 — Autenticação do ygg-api (JWT HS256)

- **Status:** ACEITO
- **Data:** 2026-10-17
- **Relacionados:** ADR-002 (chamadas síncronas; fecha a pendência de
  autenticação serviço a serviço no sentido muninn → ygg-api).

## Contexto

Até aqui todas as rotas do ygg-api aceitavam qualquer chamada que chegasse
pela rede, inclusive `POST /api/v1/incidents` (criar incidente) e
`PATCH .../verdict` (mudar a decisão de triagem). Os dois chamadores são o
muninn (serviço) e, no futuro, analistas via UI ou CLI. A mudança de verdict
precisa registrar **quem** mudou.

## Decisão

1. **Toda rota exige `Authorization: Bearer <JWT>`**, exceto `GET /healthz`
   (probes do K8s). O middleware fica em volta do mux inteiro, então uma rota
   nova nasce protegida.
2. **HS256 com segredo compartilhado** (`JWT_SECRET`). O algoritmo é fixado no
   verificador (impede `alg=none` e confusão de algoritmo). O token precisa de
   `sub` e `exp`. A folga de relógio é de 30 s.
3. **O `sub` identifica o chamador.** Ele vai para `verdict_changed_by` e para
   a tabela `incident_audit`. O corpo da requisição nunca define esse valor.
4. **Token de serviço do muninn**: um JWT com `sub=muninn`, emitido pelo
   `cmd/ygg-token` do ygg-api com o mesmo segredo, e entregue ao muninn em
   `YGG_API_SERVICE_TOKEN` (Secret do K8s). A rotação é reemitir o token antes
   do `exp`.
5. **Trava contra o segredo de dev.** O ygg-api não sobe se `ENV` for
   diferente de `development` e `JWT_SECRET` for `dev-jwt-secret` ou tiver
   menos de 32 bytes. Sem `ENV`, o padrão é `production` (falha fechada).
6. **No muninn, 401 e 403 não descartam o evento.** Token errado é erro de
   configuração, não do evento: o evento fica pendente no stream até a
   correção, como num 5xx (ver ADR-002). Limitação: a correção do token
   reinicia o pod, que volta com outro nome de consumer, e o evento fica
   preso no PEL do consumer antigo até existir `XAUTOCLAIM` (pendência do
   ADR-005).

## Alternativas consideradas

- **RS256/EdDSA com JWKS.** Separa quem assina de quem verifica, o que importa
  quando há um IdP. Adiado: hoje o ygg-api é o único verificador e não há IdP.
  A troca fica restrita ao middleware.
- **API key estática para o muninn.** Mais simples, mas sem `exp` nem `sub`
  padronizados e com um segundo mecanismo de auth para manter.
- **mTLS entre serviços.** Autentica o canal, não o usuário. Depende de uma
  service mesh ou PKI que o projeto ainda não tem.

## Consequências

- O compose roda com `ENV=development`. O `make dev-up` e o `make e2e` emitem
  os tokens com o segredo de dev.
- Quem tem o `JWT_SECRET` emite tokens com qualquer `sub`. O segredo fica só
  no Secret do ygg-api e no processo de emissão do token de serviço.
- Não há revogação de token individual: revogar é trocar o `JWT_SECRET`, o
  que invalida todos os tokens.

## Pendências

- Autenticação no sentido ygg-api → muninn (`POST /triage`). Hoje o muninn
  aceita qualquer chamada que chegue pela rede.
- Autorização por papel (ex.: só analistas mudam verdict; o muninn só cria
  incidentes). Hoje qualquer token válido acessa todas as rotas.
- Emissão de tokens para usuários (login ou IdP).
