package main

import (
	"context"
	"log"
	"strings"
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
	case stepAwaitQ2Fu1:
		// Respondeu ao "?" → delay longo (99s) antes do presente
		time.Sleep(99 * time.Second)
		if e.send(ctx, lead, msgGift) != nil {
			return
		}
		e.goTo(ctx, lead, "in_flow", stepAwaitQ3)
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
		if e.sendImageURL(ctx, lead, imgProfile, "", false) != nil {
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

	case stepAwaitQ1: // respondeu ao "gostou?" → 31s → "vc tá sozinho?"
		time.Sleep(31 * time.Second)
		if e.send(ctx, lead, msgAlone) != nil {
			return
		}
		e.goTo(ctx, lead, "in_flow", stepAwaitQ2)
		// Agenda follow-up "?" em 5 min
		_ = e.db.ScheduleAction(ctx, lead.ID, "followup", time.Now().Add(5*time.Minute), nil)

	case stepAwaitQ2: // respondeu ao "vc tá sozinho?" → 28s → presente
		time.Sleep(28 * time.Second)
		if e.send(ctx, lead, msgGift) != nil {
			return
		}
		e.goTo(ctx, lead, "in_flow", stepAwaitQ3)

	case stepAwaitQ3: // respondeu ao presente → sequência "ver ao vivo"
		time.Sleep(35 * time.Second)
		if e.send(ctx, lead, msgLive) != nil {
			return
		}
		time.Sleep(10 * time.Second)
		if e.send(ctx, lead, msgEnjoy) != nil {
			return
		}
		time.Sleep(10 * time.Second)
		if e.send(ctx, lead, msgNotAnyone) != nil {
			return
		}
		time.Sleep(6 * time.Second)
		if e.send(ctx, lead, msgLikedYou) != nil {
			return
		}
		e.goTo(ctx, lead, "in_flow", stepAwaitQ4)
		// Agenda follow-up em 3 min
		_ = e.db.ScheduleAction(ctx, lead.ID, "followup", time.Now().Add(3*time.Minute), nil)

	case stepAwaitQ4: // respondeu → 30s → sequência "liga pra mim"
		time.Sleep(30 * time.Second)
		e.sendCallSequence(ctx, lead)

	case stepCallArmed, stepCallArmed2, stepCallArmed3, stepCallArmed4:
		// Lead mandou msg em vez de ligar — log mas não avança
		log.Printf("[engine] lead %d mandou msg com call armada (step=%s)", lead.ID, lead.Step)

	case stepCallGiveUp: // desistiu de ligar
		log.Printf("[engine] lead %d em call_give_up, mandou msg", lead.ID)

	case stepAwaitQ5: // respondeu ao "topa?" → áudios + pede pix
		time.Sleep(35 * time.Second)
		if e.sendAudioURL(ctx, lead, audioYas2) != nil {
			return
		}
		time.Sleep(2 * time.Second)
		if e.sendAudioURL(ctx, lead, audioYas3) != nil {
			return
		}
		time.Sleep(6 * time.Second)
		if e.sendAudioURL(ctx, lead, audioYas4) != nil {
			return
		}
		time.Sleep(30 * time.Second)
		if e.send(ctx, lead, msgHelpToo) != nil {
			return
		}
		time.Sleep(20 * time.Second)
		if e.send(ctx, lead, msgAskPix) != nil {
			return
		}
		time.Sleep(10 * time.Second)
		if e.send(ctx, lead, msgCanSendPix) != nil {
			return
		}
		e.goTo(ctx, lead, "in_flow", stepAwaitQ6)
		// Agenda timer de 4 min (se não responder, continua mesmo assim)
		_ = e.db.ScheduleAction(ctx, lead.ID, "followup", time.Now().Add(4*time.Minute), nil)

	case stepAwaitQ6: // respondeu ao "posso te mandar meu pix?" → 29s → pix sequence
		time.Sleep(29 * time.Second)
		e.sendPixSequence(ctx, lead)

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
		// Não agenda mais nada — fica dormindo

	case stepAwaitQ2:
		// Follow-up "?" — lead não respondeu "vc tá sozinho?" em 5 min
		if e.send(ctx, lead, msgAloneFu) != nil {
			return
		}
		e.goTo(ctx, lead, "in_flow", stepAwaitQ2Fu1)
		// Não agenda mais nada — dorme até o lead voltar

	case stepAwaitQ4:
		// Timeout 3 min — lead não respondeu, continua a copy mesmo assim
		e.sendCallSequence(ctx, lead)

	case stepAwaitQ6:
		// Timeout 4 min — lead não respondeu, continua mesmo assim (sem delay extra)
		e.sendPixSequence(ctx, lead)
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

// sendImageURL — adquire o gate e envia imagem via URL com caption e viewOnce opcionais.
func (e *Engine) sendImageURL(ctx context.Context, lead *Lead, imageURL, caption string, viewOnce bool) error {
	e.gate.Acquire(lead.SessionID, lead.Phone)

	if err := e.api.SendImageURL(ctx, lead.SessionID, lead.Phone, imageURL, caption, viewOnce); err != nil {
		e.gate.Done(lead.SessionID, lead.Phone)
		log.Printf("[engine] send image lead %d: %v", lead.ID, err)
		return err
	}
	_ = e.db.InsertMessage(ctx, lead.ID, "outbound", "[image] "+caption, "image", "", lead.SessionID)
	e.db.LogEvent(ctx, lead.ID, "outbound", map[string]any{"type": "image", "url": imageURL, "view_once": viewOnce})

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

// sendCallSequence — sequência "liga pra mim" + arma auto-atender vídeo.
// Usada tanto quando o lead responde (via advance) quanto no timeout (via HandleTimer).
func (e *Engine) sendCallSequence(ctx context.Context, lead *Lead) {
	if e.send(ctx, lead, msgCallMe) != nil {
		return
	}
	time.Sleep(8 * time.Second)
	if e.send(ctx, lead, msgOnWA) != nil {
		return
	}
	time.Sleep(10 * time.Second)
	if e.send(ctx, lead, msgNotFake) != nil {
		return
	}
	time.Sleep(6 * time.Second)
	if e.send(ctx, lead, msgShowU) != nil {
		return
	}
	time.Sleep(5 * time.Second)
	if e.send(ctx, lead, msgCallNow) != nil {
		return
	}
	time.Sleep(3 * time.Second)
	if e.send(ctx, lead, msgWaiting) != nil {
		return
	}
	// Arma o auto-atender em vídeo (só aceita ligação do número do lead)
	e.armVideoCall(ctx, lead, videoCall1)
	e.goTo(ctx, lead, "in_flow", stepCallArmed)
}

// armVideoCall — arma o auto-accept de chamada de vídeo na api-escala.
func (e *Engine) armVideoCall(ctx context.Context, lead *Lead, videoURL string) {
	if err := e.api.AcceptVideo(ctx, lead.SessionID, videoURL, lead.Phone); err != nil {
		log.Printf("[engine] arm video call lead %d: %v", lead.ID, err)
		return
	}
	log.Printf("[engine] vídeo-chamada armada para lead %d (phone=%s)", lead.ID, lead.Phone)
	e.db.LogEvent(ctx, lead.ID, "outbound", map[string]any{"type": "arm_video_call", "video": videoURL, "phone": lead.Phone})
}

// sendPixSequence — áudio final + pedido de pix + envio da chave.
// Usada tanto quando o lead responde (29s delay) quanto no timeout (direto).
func (e *Engine) sendPixSequence(ctx context.Context, lead *Lead) {
	if e.sendAudioURL(ctx, lead, audioYas5) != nil {
		return
	}
	time.Sleep(58 * time.Second)
	if e.send(ctx, lead, msgSendPix) != nil {
		return
	}
	time.Sleep(12 * time.Second)
	if e.send(ctx, lead, msgCopyKey) != nil {
		return
	}
	time.Sleep(3 * time.Second)
	if e.sendPix(ctx, lead) != nil {
		return
	}
	time.Sleep(8 * time.Second)
	if e.send(ctx, lead, msgSendReceipt) != nil {
		return
	}
	time.Sleep(3 * time.Second)
	if e.send(ctx, lead, msgDealAmor) != nil {
		return
	}
	time.Sleep(11 * time.Second)
	if e.send(ctx, lead, msgWaitHeart) != nil {
		return
	}
	e.goTo(ctx, lead, "awaiting_payment", stepPixSent)
}

// HandleCallEvent — processa eventos de chamada (aceita, expirada).
func (e *Engine) HandleCallEvent(ctx context.Context, ev *CallEventJob) {
	var lead *Lead
	var err error

	if ev.Phone != "" {
		lead, err = e.db.GetLeadByPhone(ctx, ev.Phone, ev.SessionID)
	} else {
		// Expired: busca o lead em qualquer step "call_armed*"
		for _, step := range []string{stepCallArmed, stepCallArmed2, stepCallArmed3, stepCallArmed4} {
			lead, err = e.db.GetLeadByStep(ctx, ev.SessionID, step)
			if err == nil {
				break
			}
		}
	}
	if err != nil {
		log.Printf("[engine] call event %s: get lead: %v", ev.Event, err)
		return
	}

	e.db.LogEvent(ctx, lead.ID, "call", map[string]any{"event": ev.Event, "phone": ev.Phone, "step": lead.Step})

	switch ev.Event {
	case "accepted":
		// Qualquer tentativa — lead ligou → continua a copy principal
		if strings.HasPrefix(lead.Step, "call_armed") {
			log.Printf("[engine] lead %d ligou (step=%s) — continuando copy", lead.ID, lead.Step)
			e.sendPostCallSequence(ctx, lead)
		}
	case "expired":
		switch lead.Step {
		case stepCallArmed:
			log.Printf("[engine] lead %d não ligou (tentativa 1) — follow-up 1", lead.ID)
			e.sendCallFollowUp1(ctx, lead)
		case stepCallArmed2:
			log.Printf("[engine] lead %d não ligou (tentativa 2) — follow-up 2", lead.ID)
			e.sendCallFollowUp2(ctx, lead)
		case stepCallArmed3:
			log.Printf("[engine] lead %d não ligou (tentativa 3) — follow-up 3 (último)", lead.ID)
			e.sendCallFollowUp3(ctx, lead)
		case stepCallArmed4:
			log.Printf("[engine] lead %d não ligou (tentativa 4) — desistindo", lead.ID)
			e.goTo(ctx, lead, "lost", stepCallGiveUp)
		}
	}
}

// sendCallFollowUp1 — 1ª vez que não ligou.
func (e *Engine) sendCallFollowUp1(ctx context.Context, lead *Lead) {
	if e.send(ctx, lead, msgCf1a) != nil { return }
	time.Sleep(5 * time.Second)
	if e.send(ctx, lead, msgCf1b) != nil { return }
	time.Sleep(5 * time.Second)
	if e.send(ctx, lead, msgCf1c) != nil { return }
	// Re-arma vídeo-chamada
	e.armVideoCall(ctx, lead, videoCall1)
	time.Sleep(10 * time.Second)
	if e.send(ctx, lead, msgCf1d) != nil { return }
	time.Sleep(12 * time.Second)
	if e.send(ctx, lead, msgCf1e) != nil { return }
	time.Sleep(5 * time.Second)
	if e.send(ctx, lead, msgCf1f) != nil { return }
	e.goTo(ctx, lead, "in_flow", stepCallArmed2)
}

// sendCallFollowUp2 — 2ª vez que não ligou.
func (e *Engine) sendCallFollowUp2(ctx context.Context, lead *Lead) {
	if e.send(ctx, lead, msgCf2a) != nil { return }
	time.Sleep(5 * time.Second)
	if e.send(ctx, lead, msgCf2b) != nil { return }
	time.Sleep(5 * time.Second)
	if e.send(ctx, lead, msgCf2c) != nil { return }
	time.Sleep(10 * time.Second)
	if e.send(ctx, lead, msgCf2d) != nil { return }
	time.Sleep(10 * time.Second)
	// Re-arma vídeo-chamada
	e.armVideoCall(ctx, lead, videoCall1)
	if e.send(ctx, lead, msgCf2e) != nil { return }
	time.Sleep(5 * time.Second)
	if e.send(ctx, lead, msgCf2f) != nil { return }
	time.Sleep(5 * time.Second)
	if e.send(ctx, lead, msgCf2g) != nil { return }
	e.goTo(ctx, lead, "in_flow", stepCallArmed3)
}

// sendCallFollowUp3 — 3ª vez que não ligou (último follow-up).
func (e *Engine) sendCallFollowUp3(ctx context.Context, lead *Lead) {
	if e.send(ctx, lead, msgCf3a) != nil { return }
	time.Sleep(5 * time.Second)
	if e.send(ctx, lead, msgCf3b) != nil { return }
	time.Sleep(5 * time.Second)
	if e.send(ctx, lead, msgCf3c) != nil { return }
	time.Sleep(10 * time.Second)
	if e.sendImageURL(ctx, lead, imgViewOnce, "", true) != nil { return }
	time.Sleep(5 * time.Second)
	if e.send(ctx, lead, msgCf3d) != nil { return }
	time.Sleep(20 * time.Second)
	if e.send(ctx, lead, msgCf3e) != nil { return }
	time.Sleep(20 * time.Second)
	if e.send(ctx, lead, msgCf3f) != nil { return }
	time.Sleep(5 * time.Second)
	if e.send(ctx, lead, msgCf3g) != nil { return }
	// Última tentativa de vídeo-chamada
	e.armVideoCall(ctx, lead, videoCall1)
	e.goTo(ctx, lead, "in_flow", stepCallArmed4)
}

// sendPostCallSequence — sequência pós-chamada de vídeo.
func (e *Engine) sendPostCallSequence(ctx context.Context, lead *Lead) {
	time.Sleep(20 * time.Second)
	if e.send(ctx, lead, msgDoneCall) != nil {
		return
	}
	time.Sleep(5 * time.Second)
	if e.send(ctx, lead, msgLikedCall) != nil {
		return
	}
	time.Sleep(8 * time.Second)
	if e.send(ctx, lead, msgLikedU2) != nil {
		return
	}
	time.Sleep(10 * time.Second)
	if e.send(ctx, lead, msgThinking) != nil {
		return
	}
	time.Sleep(20 * time.Second)
	if e.send(ctx, lead, msgWhatAbout) != nil {
		return
	}
	time.Sleep(8 * time.Second)
	if e.send(ctx, lead, msgContinue) != nil {
		return
	}
	time.Sleep(5 * time.Second)
	if e.send(ctx, lead, msgThisCall) != nil {
		return
	}
	time.Sleep(12 * time.Second)
	if e.send(ctx, lead, msgLike) != nil {
		return
	}
	time.Sleep(3 * time.Second)
	if e.send(ctx, lead, msgBothHere) != nil {
		return
	}
	time.Sleep(6 * time.Second)
	if e.send(ctx, lead, msgJustUs) != nil {
		return
	}
	time.Sleep(8 * time.Second)
	if e.send(ctx, lead, msgWanna) != nil {
		return
	}
	e.goTo(ctx, lead, "in_flow", stepAwaitQ5)
}
