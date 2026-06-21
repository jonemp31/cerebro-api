-- ─────────────────────────────────────────────────────────────────────────────
-- cerebro-api — schema
-- Postgres 16. Aplicado automaticamente no primeiro boot via /docker-entrypoint-initdb.d.
-- ─────────────────────────────────────────────────────────────────────────────

-- updated_at automático
CREATE OR REPLACE FUNCTION set_updated_at() RETURNS TRIGGER AS $$
BEGIN
  NEW.updated_at = now();
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- ── LEADS ────────────────────────────────────────────────────────────────────
-- Quem é o lead e ONDE ele está no funil (step_atual é o ponteiro da máquina de estados).
CREATE TABLE IF NOT EXISTS leads (
    id              BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    phone           TEXT        NOT NULL,                 -- só dígitos, normalizado (ex.: 5516999999999)
    name            TEXT,
    session_id      TEXT,                                 -- instância da api-escala que atende este lead
    status          TEXT        NOT NULL DEFAULT 'new',   -- new | in_flow | awaiting_payment | paid | delivered | lost
    flow            TEXT,                                 -- nome do fluxo em execução
    step            TEXT,                                 -- passo atual dentro do fluxo
    context         JSONB       NOT NULL DEFAULT '{}',    -- dados livres por lead (respostas, flags, etc.)
    last_inbound_at TIMESTAMPTZ,                          -- última mensagem recebida do lead
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (phone, session_id)                            -- um lead por número POR instância
);
CREATE INDEX IF NOT EXISTS idx_leads_phone   ON leads(phone);
CREATE INDEX IF NOT EXISTS idx_leads_status  ON leads(status);
DROP TRIGGER IF EXISTS trg_leads_updated ON leads;
CREATE TRIGGER trg_leads_updated BEFORE UPDATE ON leads
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- ── MESSAGES ─────────────────────────────────────────────────────────────────
-- Histórico de mensagens + dedup das recebidas (a api faz retry de webhook).
CREATE TABLE IF NOT EXISTS messages (
    id              BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    lead_id         BIGINT      REFERENCES leads(id) ON DELETE CASCADE,
    direction       TEXT        NOT NULL,                 -- inbound | outbound
    body            TEXT,
    msg_type        TEXT        NOT NULL DEFAULT 'text',  -- text | image | audio | pix | ...
    wa_message_id   TEXT,                                 -- id da mensagem no WhatsApp (dedup)
    session_id      TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_messages_lead ON messages(lead_id, created_at);
-- dedup: nunca processar a mesma mensagem recebida duas vezes
CREATE UNIQUE INDEX IF NOT EXISTS uq_messages_wa_id
  ON messages(wa_message_id) WHERE wa_message_id IS NOT NULL;

-- ── SCHEDULED_ACTIONS ────────────────────────────────────────────────────────
-- Timers do funil: "se não responder até fire_at, dispare isto".
-- O worker varre WHERE status='pending' AND fire_at <= now().
CREATE TABLE IF NOT EXISTS scheduled_actions (
    id              BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    lead_id         BIGINT      NOT NULL REFERENCES leads(id) ON DELETE CASCADE,
    kind            TEXT        NOT NULL,                 -- timeout | followup | payment_check | ...
    fire_at         TIMESTAMPTZ NOT NULL,
    payload         JSONB       NOT NULL DEFAULT '{}',    -- ex.: { "step": "aguardando_aceite", "attempt": 1 }
    status          TEXT        NOT NULL DEFAULT 'pending', -- pending | fired | cancelled
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    fired_at        TIMESTAMPTZ
);
-- índice que o poller usa (parcial: só o que importa varrer)
CREATE INDEX IF NOT EXISTS idx_sched_due
  ON scheduled_actions(fire_at) WHERE status = 'pending';
CREATE INDEX IF NOT EXISTS idx_sched_lead ON scheduled_actions(lead_id);

-- ── PAYMENTS ─────────────────────────────────────────────────────────────────
-- Cobranças Pix geradas no gateway. O webhook do gateway atualiza status→'paid'.
CREATE TABLE IF NOT EXISTS payments (
    id                BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    lead_id           BIGINT      NOT NULL REFERENCES leads(id) ON DELETE CASCADE,
    gateway           TEXT,                               -- nome do gateway
    gateway_charge_id TEXT,                               -- id da cobrança no gateway (p/ casar o webhook)
    amount_cents      INTEGER,                            -- valor em centavos
    status            TEXT        NOT NULL DEFAULT 'pending', -- pending | paid | expired | failed
    br_code           TEXT,                               -- Pix copia-e-cola enviado ao lead
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    paid_at           TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_payments_charge ON payments(gateway_charge_id);
CREATE INDEX IF NOT EXISTS idx_payments_lead   ON payments(lead_id, status);

-- ── EVENTS (auditoria / debug) ───────────────────────────────────────────────
-- Log do que o cérebro recebeu/decidiu — facilita entender o histórico de um lead.
CREATE TABLE IF NOT EXISTS events (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    lead_id     BIGINT,
    type        TEXT        NOT NULL,                     -- inbound | outbound | timer_fired | payment_paid | ...
    data        JSONB       NOT NULL DEFAULT '{}',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_events_lead ON events(lead_id, created_at);
