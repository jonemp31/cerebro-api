package main

import (
	"context"
	"log"
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

// InboundJob — uma mensagem (ou lote debounced) recebida de um lead.
type InboundJob struct {
	Phone     string
	SessionID string
	Body      string     // texto combinado (se batch, msgs unidas com \n)
	WAMsgID   string     // ID da primeira mensagem (dedup)
	Name      string
	BatchMsgs []BatchMsg // mensagens individuais (preenchido pelo Debouncer; nil = msg única)
}

// HandleInbound — processa UMA interação do lead (pode conter várias msgs debounced).
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

	// Grava mensagens: individuais se debounced, única se não
	if len(j.BatchMsgs) > 1 {
		for _, m := range j.BatchMsgs {
			_ = e.db.InsertMessage(ctx, lead.ID, "inbound", m.Body, "text", m.WAMsgID, j.SessionID)
		}
	} else {
		_ = e.db.InsertMessage(ctx, lead.ID, "inbound", j.Body, "text", j.WAMsgID, j.SessionID)
	}
	e.db.LogEvent(ctx, lead.ID, "inbound", map[string]any{"body": j.Body, "step": lead.Step})

	// Cancela timers pendentes (o lead respondeu)
	_ = e.db.CancelActions(ctx, lead.ID)

	// Se o lead estava em follow-up, trata o retorno
	switch lead.Step {
	case stepAwaitQ1Fu2:
		// Voltou depois do follow-up pesado → sequência de comeback, depois continua
		e.sendComeback(ctx, lead)
		e.goTo(ctx, lead, "in_flow", stepAwaitQ1)
		e.advance(ctx, lead)
	case stepAwaitQ1Fu1:
		// Voltou depois do 1° follow-up → continua normalmente
		e.goTo(ctx, lead, "in_flow", stepAwaitQ1)
		e.advance(ctx, lead)
	default:
		e.advance(ctx, lead)
	}
}

// advance — a máquina de estados do funil (Fase 1).
func (e *Engine) advance(ctx context.Context, lead *Lead) {
	switch lead.Step {

	case stepNew: // primeiro contato → sequência de apresentação
		e.replyDelay()
		if e.send(ctx, lead, randomGreeting()) != nil {
			return
		}
		time.Sleep(10 * time.Second)
		if e.sendAudioURL(ctx, lead, audioGreeting) != nil {
			return
		}
		time.Sleep(30 * time.Second)
		if e.send(ctx, lead, msgShowYou) != nil {
			return
		}
		time.Sleep(10 * time.Second)
		if e.sendImageURL(ctx, lead, imgProfile, "") != nil {
			return
		}
		time.Sleep(5 * time.Second)
		if e.send(ctx, lead, msgThatsMe) != nil {
			return
		}
		time.Sleep(5 * time.Second)
		if e.send(ctx, lead, msgLikedIt) != nil {
			return
		}
		e.goTo(ctx, lead, "in_flow", stepAwaitQ1)
		// Agenda follow-up em 5 min (se o lead não responder)
		_ = e.db.ScheduleAction(ctx, lead.ID, "followup", time.Now().Add(5*time.Minute), nil)

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

// HandleTimer — dispara quando um timer vence (follow-ups).
func (e *Engine) HandleTimer(ctx context.Context, a *Action) {
	lead, err := e.db.GetLead(ctx, a.LeadID)
	if err != nil {
		log.Printf("[engine] timer: get lead %d: %v", a.LeadID, err)
		return
	}
	log.Printf("[engine] timer %q disparou p/ lead %d (step=%s)", a.Kind, lead.ID, lead.Step)

	switch lead.Step {
	case stepAwaitQ1:
		// 1° follow-up — lead não respondeu em 5 min
		if e.send(ctx, lead, randomFollowUp1()) != nil {
			return
		}
		e.goTo(ctx, lead, "in_flow", stepAwaitQ1Fu1)
		// Agenda 2° follow-up em mais 5 min
		_ = e.db.ScheduleAction(ctx, lead.ID, "followup", time.Now().Add(5*time.Minute), nil)

	case stepAwaitQ1Fu1:
		// 2° follow-up — lead não respondeu mais 5 min → sequência pesada
		if e.send(ctx, lead, msgFu2a) != nil {
			return
		}
		time.Sleep(3 * time.Second)
		if e.send(ctx, lead, msgFu2b) != nil {
			return
		}
		time.Sleep(3 * time.Second)
		if e.send(ctx, lead, msgFu2c) != nil {
			return
		}
		e.goTo(ctx, lead, "in_flow", stepAwaitQ1Fu2)
		// Não agenda mais nada — fica dormindo até o lead voltar
	}
}

// ── humanização ──────────────────────────────────────────────────────────────

// replyDelay — aplica o delay de "leitura" humanizado baseado no horário de SP.
func (e *Engine) replyDelay() {
	humanDelay()
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

// sendAudioURL — adquire o gate e envia áudio via URL.
// A api-escala baixa o arquivo e simula a gravação automaticamente.
func (e *Engine) sendAudioURL(ctx context.Context, lead *Lead, audioURL string) error {
	e.gate.Acquire(lead.SessionID, lead.Phone)

	if err := e.api.SendAudioURL(ctx, lead.SessionID, lead.Phone, audioURL); err != nil {
		e.gate.Done(lead.SessionID, lead.Phone)
		log.Printf("[engine] send audio lead %d: %v", lead.ID, err)
		return err
	}
	_ = e.db.InsertMessage(ctx, lead.ID, "outbound", "[audio]", "audio", "", lead.SessionID)
	e.db.LogEvent(ctx, lead.ID, "outbound", map[string]any{"type": "audio", "url": audioURL})

	e.gate.Done(lead.SessionID, lead.Phone)
	return nil
}

// sendImageURL — adquire o gate e envia imagem via URL com caption opcional.
func (e *Engine) sendImageURL(ctx context.Context, lead *Lead, imageURL, caption string) error {
	e.gate.Acquire(lead.SessionID, lead.Phone)

	if err := e.api.SendImageURL(ctx, lead.SessionID, lead.Phone, imageURL, caption); err != nil {
		e.gate.Done(lead.SessionID, lead.Phone)
		log.Printf("[engine] send image lead %d: %v", lead.ID, err)
		return err
	}
	_ = e.db.InsertMessage(ctx, lead.ID, "outbound", "[image] "+caption, "image", "", lead.SessionID)
	e.db.LogEvent(ctx, lead.ID, "outbound", map[string]any{"type": "image", "url": imageURL})

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

// sendComeback — sequência de "volta" quando o lead retorna após follow-up pesado.
func (e *Engine) sendComeback(ctx context.Context, lead *Lead) {
	e.replyDelay()
	if e.send(ctx, lead, msgComebackA) != nil {
		return
	}
	time.Sleep(5 * time.Second)
	if e.send(ctx, lead, msgComebackB) != nil {
		return
	}
	time.Sleep(5 * time.Second)
	if e.send(ctx, lead, msgComebackC) != nil {
		return
	}
	time.Sleep(30 * time.Second)
}
