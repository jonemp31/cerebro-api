package main

import (
	"context"
	"log"
)

// Engine — o cérebro. Recebe eventos (mensagem do lead, timer) e decide a próxima ação.
type Engine struct {
	db  *DB
	api *APIClient
}

func NewEngine(db *DB, api *APIClient) *Engine {
	return &Engine{db: db, api: api}
}

// InboundJob — uma mensagem recebida de um lead.
type InboundJob struct {
	Phone     string
	SessionID string
	Body      string
	WAMsgID   string
	Name      string
}

// HandleInbound — processa UMA mensagem do lead (chamado pelo worker da fila, 1 a 1).
func (e *Engine) HandleInbound(ctx context.Context, j *InboundJob) {
	// 1) cadastra (se novo) ou pega o lead existente
	lead, err := e.db.UpsertLead(ctx, j.Phone, j.SessionID, j.Name)
	if err != nil {
		log.Printf("[engine] upsert lead %s: %v", j.Phone, err)
		return
	}

	// 2) dedup — se já vimos essa mensagem, ignora (a api faz retry de webhook)
	if seen, _ := e.db.MessageSeen(ctx, j.WAMsgID); seen {
		return
	}
	_ = e.db.InsertMessage(ctx, lead.ID, "inbound", j.Body, "text", j.WAMsgID, j.SessionID)
	e.db.LogEvent(ctx, lead.ID, "inbound", map[string]any{"body": j.Body, "step": lead.Step})

	// 3) avança o funil conforme o passo atual
	e.advance(ctx, lead)
}

// advance — a máquina de estados do funil (Fase 1).
func (e *Engine) advance(ctx context.Context, lead *Lead) {
	switch lead.Step {

	case stepNew: // primeiro contato → cumprimenta
		if e.send(ctx, lead, msgGreeting) != nil {
			return // não avança: o próximo evento reenvia
		}
		e.goTo(ctx, lead, "in_flow", stepAwaitQ1)

	case stepAwaitQ1: // respondeu o "Oi, tudo bem?" → faz a pergunta
		if e.send(ctx, lead, msgQuestion) != nil {
			return
		}
		e.goTo(ctx, lead, "in_flow", stepAwaitQ2)

	case stepAwaitQ2: // respondeu a pergunta → manda o Pix
		if e.send(ctx, lead, msgPixIntro) != nil {
			return
		}
		if err := e.api.SendPix(ctx, lead.SessionID, lead.Phone, pixKeyType, pixName, pixKey, ""); err != nil {
			log.Printf("[engine] send pix lead %d: %v", lead.ID, err)
			return // não avança: reenvia no próximo evento
		}
		_ = e.db.InsertMessage(ctx, lead.ID, "outbound", "[pix]", "pix", "", lead.SessionID)
		e.db.LogEvent(ctx, lead.ID, "outbound", map[string]any{"type": "pix"})
		e.goTo(ctx, lead, "awaiting_payment", stepPixSent)

	case stepPixSent: // já mandou o Pix — aguardando pagamento (próxima fase)
		log.Printf("[engine] lead %d já no passo pix_sent (aguardando pagamento)", lead.ID)

	default:
		log.Printf("[engine] passo desconhecido %q (lead %d)", lead.Step, lead.ID)
	}
}

// HandleTimer — dispara quando um timer vence (esperas/follow-ups). Stub na Fase 1.
func (e *Engine) HandleTimer(ctx context.Context, a *Action) {
	log.Printf("[engine] timer %q disparou p/ lead %d (follow-ups entram com a copy de follow-up)", a.Kind, a.LeadID)
}

// helpers ────────────────────────────────────────────────────────────────────

func (e *Engine) send(ctx context.Context, lead *Lead, text string) error {
	if err := e.api.SendText(ctx, lead.SessionID, lead.Phone, text); err != nil {
		log.Printf("[engine] send text lead %d: %v", lead.ID, err)
		return err
	}
	_ = e.db.InsertMessage(ctx, lead.ID, "outbound", text, "text", "", lead.SessionID)
	e.db.LogEvent(ctx, lead.ID, "outbound", map[string]any{"body": text})
	return nil
}

func (e *Engine) goTo(ctx context.Context, lead *Lead, status, step string) {
	if err := e.db.UpdateStep(ctx, lead.ID, status, step); err != nil {
		log.Printf("[engine] update step lead %d: %v", lead.ID, err)
		return
	}
	lead.Status, lead.Step = status, step
}
