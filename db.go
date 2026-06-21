package main

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type DB struct{ pool *pgxpool.Pool }

// Lead — o estado de um lead no funil.
type Lead struct {
	ID        int64
	Phone     string
	SessionID string
	Status    string
	Flow      string
	Step      string
}

// Action — uma ação agendada (timer / follow-up).
type Action struct {
	ID      int64
	LeadID  int64
	Phone   string
	Kind    string
	Payload map[string]any
}

func NewDB(ctx context.Context, dsn string) (*DB, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		return nil, err
	}
	return &DB{pool: pool}, nil
}

func (d *DB) Close() { d.pool.Close() }

// UpsertLead — cria o lead se não existe, ou retorna o existente (com o passo atual).
// A constraint UNIQUE(phone, session_id) garante: nunca duplica.
func (d *DB) UpsertLead(ctx context.Context, phone, sessionID, name string) (*Lead, error) {
	const q = `
		INSERT INTO leads (phone, session_id, name, status, last_inbound_at)
		VALUES ($1, $2, NULLIF($3,''), 'new', now())
		ON CONFLICT (phone, session_id) DO UPDATE
		   SET last_inbound_at = now(),
		       name = COALESCE(leads.name, NULLIF($3,''))
		RETURNING id, phone, session_id, status, COALESCE(flow,''), COALESCE(step,'')`
	var l Lead
	err := d.pool.QueryRow(ctx, q, phone, sessionID, name).
		Scan(&l.ID, &l.Phone, &l.SessionID, &l.Status, &l.Flow, &l.Step)
	if err != nil {
		return nil, err
	}
	return &l, nil
}

// UpdateStep — avança o lead para um novo status/passo.
func (d *DB) UpdateStep(ctx context.Context, leadID int64, status, step string) error {
	_, err := d.pool.Exec(ctx,
		`UPDATE leads SET status=$2, step=$3 WHERE id=$1`, leadID, status, step)
	return err
}

// MessageSeen — true se já processamos essa mensagem (dedup; a api faz retry de webhook).
func (d *DB) MessageSeen(ctx context.Context, waID string) (bool, error) {
	if waID == "" {
		return false, nil
	}
	var exists bool
	err := d.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM messages WHERE wa_message_id=$1)`, waID).Scan(&exists)
	return exists, err
}

func (d *DB) InsertMessage(ctx context.Context, leadID int64, dir, body, msgType, waID, sessionID string) error {
	_, err := d.pool.Exec(ctx,
		`INSERT INTO messages (lead_id, direction, body, msg_type, wa_message_id, session_id)
		 VALUES ($1,$2,$3,$4,NULLIF($5,''),$6)`,
		leadID, dir, body, msgType, waID, sessionID)
	return err
}

func (d *DB) LogEvent(ctx context.Context, leadID int64, typ string, data map[string]any) {
	_, _ = d.pool.Exec(ctx,
		`INSERT INTO events (lead_id, type, data) VALUES ($1,$2,$3)`, leadID, typ, data)
}

// ── scheduled_actions (timers) — infra pronta p/ os follow-ups ────────────────

func (d *DB) ScheduleAction(ctx context.Context, leadID int64, kind string, fireAt time.Time, payload map[string]any) error {
	_, err := d.pool.Exec(ctx,
		`INSERT INTO scheduled_actions (lead_id, kind, fire_at, payload) VALUES ($1,$2,$3,$4)`,
		leadID, kind, fireAt, payload)
	return err
}

// CancelActions — cancela timers pendentes do lead (ex.: ele respondeu, não precisa do follow-up).
func (d *DB) CancelActions(ctx context.Context, leadID int64) error {
	_, err := d.pool.Exec(ctx,
		`UPDATE scheduled_actions SET status='cancelled' WHERE lead_id=$1 AND status='pending'`, leadID)
	return err
}

// DueActions — pega (e marca como 'fired') as ações vencidas, de forma atômica.
func (d *DB) DueActions(ctx context.Context) ([]Action, error) {
	const q = `
		WITH due AS (
		  SELECT id FROM scheduled_actions
		  WHERE status='pending' AND fire_at <= now()
		  ORDER BY fire_at
		  LIMIT 100
		  FOR UPDATE SKIP LOCKED
		), upd AS (
		  UPDATE scheduled_actions SET status='fired', fired_at=now()
		  WHERE id IN (SELECT id FROM due)
		  RETURNING id, lead_id, kind, payload
		)
		SELECT u.id, u.lead_id, u.kind, u.payload, l.phone
		FROM upd u JOIN leads l ON l.id = u.lead_id`
	rows, err := d.pool.Query(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Action
	for rows.Next() {
		var a Action
		if err := rows.Scan(&a.ID, &a.LeadID, &a.Kind, &a.Payload, &a.Phone); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (d *DB) GetLead(ctx context.Context, id int64) (*Lead, error) {
	var l Lead
	err := d.pool.QueryRow(ctx,
		`SELECT id, phone, session_id, status, COALESCE(flow,''), COALESCE(step,'') FROM leads WHERE id=$1`, id).
		Scan(&l.ID, &l.Phone, &l.SessionID, &l.Status, &l.Flow, &l.Step)
	if err != nil {
		return nil, err
	}
	return &l, nil
}

// GetLeadByPhone — busca lead por telefone + sessão (sem upsert).
func (d *DB) GetLeadByPhone(ctx context.Context, phone, sessionID string) (*Lead, error) {
	var l Lead
	err := d.pool.QueryRow(ctx,
		`SELECT id, phone, session_id, status, COALESCE(flow,''), COALESCE(step,'')
		 FROM leads WHERE phone=$1 AND session_id=$2`, phone, sessionID).
		Scan(&l.ID, &l.Phone, &l.SessionID, &l.Status, &l.Flow, &l.Step)
	if err != nil {
		return nil, err
	}
	return &l, nil
}

// GetLeadByStep — busca lead por sessão + passo (ex: achar quem está em call_armed).
func (d *DB) GetLeadByStep(ctx context.Context, sessionID, step string) (*Lead, error) {
	var l Lead
	err := d.pool.QueryRow(ctx,
		`SELECT id, phone, session_id, status, COALESCE(flow,''), COALESCE(step,'')
		 FROM leads WHERE session_id=$1 AND step=$2 LIMIT 1`, sessionID, step).
		Scan(&l.ID, &l.Phone, &l.SessionID, &l.Status, &l.Flow, &l.Step)
	if err != nil {
		return nil, err
	}
	return &l, nil
}

// ── payments ────────────────────────────────────────────────────────────────

// InsertPayment — registra uma cobrança Pix no banco.
func (d *DB) InsertPayment(ctx context.Context, leadID int64, gateway, chargeID string, amountCents int, brCode string) error {
	_, err := d.pool.Exec(ctx,
		`INSERT INTO payments (lead_id, gateway, gateway_charge_id, amount_cents, br_code, status)
		 VALUES ($1, $2, $3, $4, $5, 'pending')`,
		leadID, gateway, chargeID, amountCents, brCode)
	return err
}

// GetPendingPayment — retorna o charge_id do pagamento pendente mais recente do lead.
func (d *DB) GetPendingPayment(ctx context.Context, leadID int64) (string, error) {
	var chargeID string
	err := d.pool.QueryRow(ctx,
		`SELECT gateway_charge_id FROM payments
		 WHERE lead_id=$1 AND status='pending'
		 ORDER BY created_at DESC LIMIT 1`, leadID).Scan(&chargeID)
	return chargeID, err
}

// UpdatePaymentStatus — atualiza o status de um pagamento (paid, expired, cancelled).
func (d *DB) UpdatePaymentStatus(ctx context.Context, chargeID, status string) error {
	q := `UPDATE payments SET status=$2`
	if status == "paid" {
		q += `, paid_at=now()`
	}
	q += ` WHERE gateway_charge_id=$1`
	_, err := d.pool.Exec(ctx, q, chargeID, status)
	return err
}
