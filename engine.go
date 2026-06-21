package main

import (
	"context"
	"log"
	"math/rand"
	"time"
)

// Engine — o cérebro. Recebe eventos (mensagem do lead, timer) e decide a ação.
type Engine struct {
	db   *DB
	api  *APIClient
	gate *SendGate
}

func NewEngine(db *DB, api *APIClient, gate *SendGate) *Engine {
	return &Engine{db: db, api: api, gate: gate}
}

// InboundJob — uma mensagem recebida de um lead.
type InboundJob struct {
	Phone     string
	SessionID string
	Body      string
	WAMsgID   string
	Name      string
}

// HandleInbound — processa UMA mensagem do lead (serial por lead, via KeyedMutex).
func (e *Engine) HandleInbound(ctx context.Context, j *InboundJob) {
	lead, err := e.db.UpsertLead(ctx, j.Phone, j.SessionID, j.Name)
	if err != nil {
		log.Printf("[engine] upsert lead %s: %v", j.Phone, err)
		return
	}
	// dedup — a api faz retry de webhook (feito ANTES dos delays)
	if seen, _ := e.db.MessageSeen(ctx, j.WAMsgID); seen {
		return
	}
	_ = e.db.InsertMessage(ctx, lead.ID, "inbound", j.Body, "text", j.WAMsgID, j.SessionID)
	e.db.LogEvent(ctx, lead.ID, "inbound", map[string]any{"body": j.Body, "step": lead.Step})

	e.advance(ctx, lead)
}

// advance — a máquina de estados do funil (Fase 1).
func (e *Engine) advance(ctx context.Context, lead *Lead) {
	switch lead.Step {

	case stepNew: // primeiro contato → cumprimenta
		e.replyDelay()
		if e.send(ctx, lead, msgGreeting) != nil {
			return
		}
		e.goTo(ctx, lead, "in_flow", stepAwaitQ1)

	case stepAwaitQ1: // respondeu → faz a pergunta
		e.replyDelay()
		if e.send(ctx, lead, msgQuestion) != nil {
			return
		}
		e.goTo(ctx, lead, "in_flow", stepAwaitQ2)

	case stepAwaitQ2: // respondeu → manda o Pix
		e.replyDelay()
		if e.send(ctx, lead, msgPixIntro) != nil {
			return
		}
		if e.sendPix(ctx, lead) != nil {
			return
		}
		e.goTo(ctx, lead, "awaiting_payment", stepPixSent)

	case stepPixSent: // aguardando pagamento (próxima fase)
		log.Printf("[engine] lead %d já no passo pix_sent (aguardando pagamento)", lead.ID)

	default:
		log.Printf("[engine] passo desconhecido %q (lead %d)", lead.Step, lead.ID)
	}
}

// HandleTimer — dispara quando um timer vence (esperas/follow-ups). Stub na Fase 1.
func (e *Engine) HandleTimer(ctx context.Context, a *Action) {
	log.Printf("[engine] timer %q disparou p/ lead %d (follow-ups entram com a copy de follow-up)", a.Kind, a.LeadID)
}

// ── humanização ──────────────────────────────────────────────────────────────

// replyDelay — espera "lendo/pensando" (random entre min e max) antes de responder.
func (e *Engine) replyDelay() {
	d := cfgMinDelay
	if cfgMaxDelay > cfgMinDelay {
		d += time.Duration(rand.Int63n(int64(cfgMaxDelay - cfgMinDelay)))
	}
	time.Sleep(d)
}

// typingFor — duração do "digitando..." proporcional ao texto (base + por-char, com teto).
func typingFor(text string) time.Duration {
	d := cfgTypingBase + time.Duration(len([]rune(text)))*cfgTypingPerChar
	if d > cfgTypingCap {
		d = cfgTypingCap
	}
	return d
}

// typing — aciona o "digitando..." na api e espera a duração (a api não bloqueia).
func (e *Engine) typing(ctx context.Context, lead *Lead, d time.Duration) {
	chatID := lead.Phone + "@c.us"
	_ = e.api.SendTyping(ctx, lead.SessionID, chatID, int(d.Milliseconds()))
	time.Sleep(d)
}

// send — adquire o gate da sessão, mostra "digitando...", espera, e envia o texto.
// O gate garante que só 1 lead por vez "digita" numa sessão — comportamento humano.
func (e *Engine) send(ctx context.Context, lead *Lead, text string) error {
	e.gate.Acquire(lead.SessionID, lead.Phone)

	e.typing(ctx, lead, typingFor(text))
	if err := e.api.SendText(ctx, lead.SessionID, lead.Phone, text); err != nil {
		e.gate.Done(lead.SessionID, lead.Phone)
		log.Printf("[engine] send text lead %d: %v", lead.ID, err)
		return err
	}
	_ = e.db.InsertMessage(ctx, lead.ID, "outbound", text, "text", "", lead.SessionID)
	e.db.LogEvent(ctx, lead.ID, "outbound", map[string]any{"body": text})

	e.gate.Done(lead.SessionID, lead.Phone)
	return nil
}

// sendPix — adquire o gate, "digitando..." curto e envia o card de Pix.
func (e *Engine) sendPix(ctx context.Context, lead *Lead) error {
	e.gate.Acquire(lead.SessionID, lead.Phone)

	e.typing(ctx, lead, cfgTypingBase)
	if err := e.api.SendPix(ctx, lead.SessionID, lead.Phone, pixKeyType, pixName, pixKey, ""); err != nil {
		e.gate.Done(lead.SessionID, lead.Phone)
		log.Printf("[engine] send pix lead %d: %v", lead.ID, err)
		return err
	}
	_ = e.db.InsertMessage(ctx, lead.ID, "outbound", "[pix]", "pix", "", lead.SessionID)
	e.db.LogEvent(ctx, lead.ID, "outbound", map[string]any{"type": "pix"})

	e.gate.Done(lead.SessionID, lead.Phone)
	return nil
}

func (e *Engine) goTo(ctx context.Context, lead *Lead, status, step string) {
	if err := e.db.UpdateStep(ctx, lead.ID, status, step); err != nil {
		log.Printf("[engine] update step lead %d: %v", lead.ID, err)
		return
	}
	lead.Status, lead.Step = status, step
}
