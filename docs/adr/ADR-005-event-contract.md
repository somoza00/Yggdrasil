# ADR-005 — Contrato de eventos assíncrono (Redis Streams)

- **Status:** ACEITO
- **Data:** 2026-10-15
- **Relacionados:** ADR-003 (Redis Streams), ADR-004 (camada de IA). Independe
  do ADR-002, que trata só do protocolo **síncrono**.

## Contexto

O fluxo principal (huginn → muninn) é assíncrono via Redis Streams (ADR-003),
mas não havia contrato escrito. A auditoria encontrou os dois lados
incompatíveis:

| | huginn emitia | muninn esperava |
|---|---|---|
| identificador | `id` (opcional, vindo da fonte) | `alert_id` (obrigatório) |
| origem | `source` | `source` |
| título | — | `title` (obrigatório) |
| severidade | — | `severity` (obrigatório) |
| log original | `raw` | `raw` |
| parsing | `parsed` | — |
| IOCs | `[{kind, value}]` com `kind` ∈ `Ipv4, Ipv6, Domain, Sha256, Sha1, Md5, Email` | `list[str]` |
| outros | — | `tags` |

Nenhum evento do huginn validava no modelo do muninn. Além disso, o huginn
publicava em `<stream de entrada>:normalized`, nome derivado e não declarado em
lugar nenhum.

## Decisão

### 1. Streams

| Stream | Produtor | Consumidor (consumer group) | Conteúdo |
|---|---|---|---|
| `events:ingest:raw` | conectores externos (EDR, SIEM, syslog, webhooks) | huginn (`huginn`) | evento bruto |
| `events:ingest:normalized` | huginn | muninn (`muninn`) | `NormalizedEvent` |

Os nomes são configuráveis (`REDIS_STREAM_INGEST`, `REDIS_STREAM_NORMALIZED`),
mas os valores acima são o padrão em código, `.env.example`, compose e
kustomize.

### 2. Entrada: `events:ingest:raw`

Campos planos da entrada do stream, para que qualquer conector publique com um
`XADD` simples, sem montar JSON:

| Campo | Obrigatório | Descrição |
|---|---|---|
| `raw` | sim | Log original (CEF, syslog, texto livre). |
| `source` | recomendado | Conector ou formato: `cef`, `syslog*` ou outro identificador. Ausente ou vazio vira `unknown`. Define o parser usado. |
| `id` | não | Identificador do evento na fonte. Na 1.0 só vai para o log do huginn. |

### 3. Saída: `events:ingest:normalized`

Cada entrada tem **um único campo**, `event`, com o JSON (UTF-8) de um
`NormalizedEvent`:

```
XADD events:ingest:normalized * event '{"schema_version":"1.0", ...}'
```

Campo único em vez de campos planos porque `parsed` e `iocs` são aninhados, e
campos de stream só guardam strings.

### 4. `NormalizedEvent` 1.0

O schema está em [`proto/events/normalized_event.schema.json`](../../proto/events/normalized_event.schema.json)
(JSON Schema 2020-12). **Todos os campos são obrigatórios**: o produtor sempre
emite todos, e o consumidor nunca precisa tratar ausência.

| Campo | Tipo | Regra |
|---|---|---|
| `schema_version` | string | `"1.0"` (padrão `^1\.\d+$`) |
| `event_id` | string UUID | gerado pelo huginn |
| `source` | string, 1–64 chars | como recebido; `unknown` se ausente |
| `title` | string, 1–256 chars | derivado pelo huginn; nunca vazio |
| `severity_hint` | enum | `unknown`, `info`, `low`, `medium`, `high`, `critical` |
| `raw` | string | log original, sem alteração |
| `parsed` | object | sempre tem `kind`; em falha de parsing, também `error` |
| `iocs` | array de `{kind, value}` | `kind` ∈ `ip`, `domain`, `hash`, `url`; `value` não vazio |

#### Por que cada nome (resolve a divergência)

- **`schema_version`**: string `MAJOR.MINOR` em vez de inteiro, para distinguir
  mudança aditiva (minor) de quebra (major). Vai em **cada mensagem**, e não no
  nome do stream, porque durante um rollout o mesmo stream terá versões
  misturadas.
- **`event_id`**, e não `id` nem `alert_id`. `id` é ambíguo com o ID da entrada
  do Redis e com o `id` da fonte. `alert_id` é semanticamente errado: nesse
  ponto o item é um *evento*, e só vira alerta ou incidente depois da decisão
  de triagem (ygg-api). O id da fonte não serve: pode faltar e não é único
  entre fontes. Por isso o huginn gera o UUID.
  - **UUIDv5 determinístico** sobre `<stream raw>/<ID da entrada>`, com
    namespace fixo no código. A entrega é *at-least-once*: se o `XADD` funciona
    e o `XACK` falha, a mesma entrada é reprocessada. Como o id é
    determinístico, a duplicata sai com o **mesmo** `event_id`, e o consumidor
    deduplica por ele. Um UUIDv4 geraria duplicatas indistinguíveis.
- **`source`**: os dois lados já usavam esse nome. Fica como string livre, não
  enum, para que um conector novo não exija versão nova do contrato.
- **`title`**: o muninn precisava (prompt, heurística, incidente) e o huginn não
  produzia. Agora o huginn deriva, nesta ordem: `Name` do header CEF; mensagem
  syslog; primeira linha não vazia do `raw`; `"<source> event"`. Truncado em
  256 caracteres.
- **`severity_hint`**, e não `severity`. As escalas das fontes diferem (CEF
  0–10 ou Low…Very-High; syslog 0–7), a normalização perde informação e o
  valor vem da fonte, que pode estar errada ou ser controlada por um atacante.
  O sufixo deixa explícito que é **entrada** da triagem, não saída. A
  severidade decidida é a do incidente no ygg-api.
  - CEF: 0–3 → `low`, 4–6 → `medium`, 7–8 → `high`, 9–10 e `Very-High` →
    `critical`.
  - syslog: 0–2 → `critical`, 3 → `high`, 4 → `medium`, 5 → `low`, 6–7 →
    `info`.
  - Qualquer outro caso → `unknown`.
- **`raw`**: os dois lados já usavam esse nome. É repassado intacto e **não é
  confiável**: o conteúdo é controlado pela fonte. Consumidores que o colocam
  em prompt de LLM precisam delimitá-lo como dado.
- **`parsed`**: mantido do huginn e antes ignorado pelo muninn. É um objeto com
  `kind` (`cef`, `syslog`, `unknown`). O resto da forma é específico por fonte
  e **não é estável na 1.x**: serve de contexto, mas nenhuma regra de decisão
  pode depender das chaves dele. O `raw` não é repetido aqui.
- **`iocs`** como objetos `{kind, value}`, e não `list[str]`. O tipo é
  informação de que a triagem e os playbooks precisam (bloquear IP ≠ bloquear
  domínio). Uma lista de strings perde o tipo e obrigaria o muninn a
  reimplementar a detecção do huginn.
  - O enum é **grosso** (`ip`, `domain`, `hash`, `url`), porque o detalhe
    (IPv4/IPv6, MD5/SHA-1/SHA-256) é derivável do próprio valor.
  - E-mail, que o huginn detecta, **fica fora da 1.0** e não é emitido.
    Adicioná-lo depois é uma mudança minor (ver a regra 2).
- **`tags`** (do muninn) saiu: ninguém produzia. Se voltar, entra como campo
  novo numa minor.

### 5. Versionamento e compatibilidade futura

1. **Consumidores DEVEM ignorar campos desconhecidos**, no topo do evento e
   dentro de `iocs[]` e `parsed`. O schema declara
   `additionalProperties: true`.
2. **Valores de enum desconhecidos são tolerados.** Um IOC com `kind`
   desconhecido é descartado, e um `severity_hint` desconhecido vira
   `unknown`. Isso permite adicionar valores numa minor.
3. Consumidores aceitam qualquer `1.<n>` e **rejeitam outra major** (o evento
   não é processado e vai para o log).
4. **Minor** (1.0 → 1.1): só mudanças aditivas, ou seja, campo novo ou valor
   de enum novo. Consumidores 1.0 continuam funcionando.
5. **Major** (→ 2.0): remover, renomear ou mudar o tipo de um campo, ou mudar
   a semântica de um campo existente. Exige um ADR novo e uma janela de
   transição em que o produtor publica nas duas versões, em streams separados
   (por exemplo, `events:ingest:normalized:v2`).

### 6. Semântica de entrega

- **At-least-once.** O huginn só faz `XACK` na entrada raw depois de um `XADD`
  bem-sucedido na saída. O muninn deve fazer `XACK` só depois de registrar a
  decisão.
- Duplicatas são possíveis. A deduplicação é feita por `event_id`.
- Não há garantia de ordem entre eventos processados por réplicas diferentes.

### 7. Testes de contrato

- As fixtures em `proto/events/fixtures/` (`valid_cef.json`,
  `valid_syslog.json`, `minimal.json`) são a **saída exata do huginn** para
  entradas fixas.
- **huginn** (`src/ingest/mod.rs`): testes golden comparam a saída com as
  fixtures. Outros testes conferem que os campos serializados são exatamente
  os `required` do schema e que os enums Rust coincidem com os do schema.
- **muninn** (`tests/test_contract.py`): valida as mesmas fixtures contra o
  JSON Schema (`jsonschema`) e contra `AlertNormalized`, e testa as regras de
  compatibilidade futura e de rejeição.
- Qualquer mudança no contrato toca o schema, as fixtures e os dois lados, e as
  duas suítes rodam no CI. Um lado não diverge do outro sem quebrar o build.

## Alternativas consideradas

- **Adotar os nomes do muninn** (`alert_id`, `severity`, `iocs: list[str]`).
  Rejeitada: os nomes descrevem o resultado da triagem, não um evento, e a
  lista de strings perde informação.
- **Protobuf ou Avro com schema registry.** Adiada. O ADR-002 (síncrono) segue
  em aberto, e um registry seria mais um componente para operar. JSON é
  legível via `redis-cli` e o JSON Schema é validável nas três linguagens. Se
  o ADR-002 decidir gRPC, dá para gerar um `.proto` equivalente numa major
  futura.
- **Versão no nome do stream** em vez de no payload. Rejeitada para minors:
  obrigaria a trocar de stream a cada mudança aditiva. Fica só para majors
  (regra 5).

## Consequências

- huginn e muninn passam a falar o mesmo formato, verificado por testes nos
  dois lados.
- Mudam os nomes dos streams: `events:ingest` → `events:ingest:raw`, e
  `events:ingest:normalized` passa a ser declarado em vez de derivado.
  Conectores que publicavam em `events:ingest` precisam trocar o destino.
- O `POST /triage` do muninn, que recebe `AlertNormalized`, passa a exigir o
  formato 1.0.
- O detalhe de IOC (IPv4/IPv6, tipo de hash) e o e-mail saem do fio na 1.0.

## Pendências (fora deste ADR)

- O consumidor do muninn em `events:ingest:normalized` (group `muninn`) ainda
  não existe. Hoje o muninn só expõe `POST /triage`.
- `source_event_id` (o `id` da fonte) como campo opcional na 1.1, para
  rastreabilidade até o evento original.
- Retenção dos streams (`MAXLEN`/`MINID`), dead-letter stream para eventos
  rejeitados e `XAUTOCLAIM` de entradas presas no PEL.
- Validação do schema em runtime no produtor. Hoje a garantia vem dos testes.
