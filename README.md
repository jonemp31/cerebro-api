# cerebro-api 🧠

Orquestrador de atendimento inteligente para leads de WhatsApp.
É o **cérebro** que controla a [api-escala](../api-escala) (via REST + webhook) e decide,
por lead, qual a próxima ação do funil — com base no comportamento e no tempo.

> **Projeto paralelo e desacoplado.** O `cerebro-api` **não altera nada** na api-escala.
> Ele apenas (1) **recebe** o webhook de mensagens da api e (2) **chama** os endpoints
> `/send/*` da api para agir. A integração é 100% via HTTP.

---

## 1. O que ele faz (o funil)

```
Lead entra em contato
   → cadastra (se novo)
   → inicia a sequência de mensagens (copy)
   → em pontos-chave, AGUARDA a resposta do lead por X tempo
        • respondeu  → continua a copy (rota conforme regra simples)
        • não respondeu em X min → follow-up ("tá por aí? sumiu...") → aguarda +X min
   → envia o Pix de compra (BR Code copia-e-cola do gateway)
        • pagou (webhook do gateway)  → ENTREGA
        • não pagou                   → follow-up de pagamento
```

---

## 2. Arquitetura

O cérebro é uma **máquina de estados por lead**, movida por **3 fontes de evento**:

```
                          ┌──────────────────────── cerebro-api ────────────────────────┐
                          │                                                              │
  (A) Mensagem do lead    │   POST /webhook/wa                                           │
  ── webhook da api ─────▶│        │                                                     │
                          │        ▼                                                     │
  (C) Pix pago            │   POST /webhook/pay        ┌─────────────────┐    HTTP       │
  ── webhook do gateway ─▶│        │  ───────────────▶ │     ENGINE       │ ───────────▶ ├──▶ api-escala
                          │        │                   │ (máquina de      │  /send/text  │    /send/pix ...
                          │        │            ┌────▶ │   estados)       │  /send/pix   │
  (B) Tempo / espera      │        │            │      └────────┬────────┘              │
                          │   ┌────┴──────────┐ │               │                        │
                          │   │  SCHEDULER     │─┘               ▼                        │
                          │   │  (poll ~30s)   │        ┌─────────────────┐              │
                          │   │ scheduled_     │◀───────│   POSTGRES       │              │
                          │   │   actions       │        │   (local)        │              │
                          │   └────────────────┘        └─────────────────┘              │
                          └──────────────────────────────────────────────────────────────┘
```

- **(A) Mensagem do lead** → a api-escala envia `whatsapp_message_received` para `POST /webhook/wa`.
- **(B) Tempo** → tabela `scheduled_actions`; um worker varre a cada ~30s e dispara o que venceu (timeouts, follow-ups).
- **(C) Pagamento** → o gateway envia "pago" para `POST /webhook/pay` → dispara a entrega.
- O **Engine** lê o estado do lead, decide o próximo passo e **age** chamando a api-escala.

### Como as mensagens chegam (sem mexer na api)

Cada **instância** conectada na api-escala precisa ter o `webhook_url` apontando para o cérebro.
Isso é **configuração de runtime** (não é código):

```
PATCH http://localhost:3000/sessions/<ID>/webhook
  { "webhook_url": "http://host.docker.internal:8090/webhook/wa" }
```

O payload já vem com o **número resolvido** do lead e o `session_id` da instância — então
o cérebro sabe **quem** falou e **por qual instância responder**.

---

## 3. Modelo de dados (Postgres)

| Tabela | Para quê |
|---|---|
| `leads` | quem é o lead, número, instância, status, **passo atual** do funil, contexto |
| `messages` | histórico de mensagens (entrada/saída) + **dedup** de inbound (`wa_message_id`) |
| `scheduled_actions` | **timers**: "disparar follow-up às 14:10 se não responder" |
| `payments` | cobranças Pix do gateway (BR Code, status, pago_em) |
| `events` | log de auditoria (debug do que o cérebro decidiu) |

Schema completo e comentado em [`db/schema.sql`](db/schema.sql).

---

## 4. O mecanismo de espera (o coração do "aguarda X minutos")

Padrão **estado + ação agendada + cancelamento**:

1. Cérebro manda "Aceita?" → seta `leads.step = aguardando_aceite` + insere `scheduled_actions`
   com `fire_at = agora + 10min`, `kind = timeout`.
2. **Se o lead responde antes** → o webhook avança o estado e **cancela** a ação pendente.
3. **Se o timeout dispara** (sem resposta) → manda o follow-up e agenda **+10 min**.

Esse mesmo padrão cobre **qualquer** "espera X, senão faz Y" do funil — inclusive a espera
do pagamento.

---

## 5. Decisões (já fechadas)

| Tema | Decisão |
|---|---|
| **Pagamento** | BR Code copia-e-cola do **gateway** enviado pelo `/send/pix` (como "chave aleatória"); confirmação via **webhook do gateway** |
| **Banco** | **Postgres local** (parrudo), schema portável p/ Supabase no futuro |
| **Fluxo** | **fixo no código** agora; **construtor de fluxos** (data-driven) no futuro |
| **IA/LLM** | **não** por enquanto — regras simples (respondeu? contém "sim/quero"? etc.) |
| **api-escala** | **intocada** — integração só por HTTP (REST + webhook) |
| **Stack** | ⏳ *a confirmar* (recomendo **Go**, p/ consistência com a api — ou Node, se preferir iterar mais rápido) |

---

## 6. Plano em fases (MVP primeiro)

### ✅ Fase 0 — Fundação (esta entrega)
Pasta + arquitetura + **Postgres parrudo** (docker-compose) + **schema** + `.env`.

### 🎯 Fase 1 — MVP do funil (caminho feliz)
- `POST /webhook/wa`: recebe msg → **cadastra lead** (se novo) → inicia o fluxo.
- **Engine** + **um fluxo fixo** (a copy que você vai me passar): passos de `enviar` e de
  `aguardar_resposta`.
- **Scheduler** (poll ~30s) para as esperas de 10 min + follow-ups.
- **Cliente da api-escala** (enviar texto/pix).
- **Pagamento**: `POST /webhook/pay` → marca pago → **entrega**.
- Regras simples de roteamento (palavras-chave).

### 🛡️ Fase 2 — Robustez
Dedup/idempotência (webhook da api faz retry!), tratamento de erro, retries de envio,
logs/observabilidade, endpoints de admin (listar leads, ver estado de um lead).

### 🧩 Fase 3 — Construtor de fluxos (data-driven)
Mover o fluxo do código para o banco (definição de fluxo) + UI para você editar copy/tempos
sem mexer no código.

### 🤖 Fase 4 (futuro) — Inteligência (LLM)
Classificar a intenção da resposta livre do lead (interessado / objeção / dúvida / não quer)
e rotear a copy. Add-on limpo sobre o Engine.

---

## 7. Como rodar (Fase 0 — só o banco)

```bash
cp .env.example .env          # ajuste a senha do Postgres
docker compose up -d postgres # sobe o Postgres parrudo
# schema é aplicado automaticamente no primeiro boot (db/schema.sql)
```

---

## 8. O que eu preciso de você para a Fase 1

1. **A copy do funil** — a sequência de mensagens, os pontos de espera (e quantos minutos),
   o valor do Pix e o que é "entregar".
2. **O gateway** — qual é, e como gerar a cobrança Pix (BR Code) + o formato do **webhook de
   confirmação** (ou as credenciais/docs).
3. **Confirmar a stack** (Go ou Node).

Com isso eu monto o Engine + o fluxo + o scheduler e a gente testa ponta a ponta.
