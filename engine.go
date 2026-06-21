package main

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"
)

// Engine — o cérebro. Recebe eventos (mensagem do lead, timer) e decide a ação.
type Engine struct {
	db    *DB
	api   *APIClient
	gate  *SendGate
	pay   *PaymentClient
	calls *CallTracker
	once  sync.Map // flags one-shot (call_warn:ID, call_revival:ID)
}

func NewEngine(db *DB, api *APIClient, gate *SendGate, pay *PaymentClient, calls *CallTracker) *Engine {
	return &Engine{db: db, api: api, gate: gate, pay: pay, calls: calls}
}

// isArmedStep — retorna true se o step é de chamada armada (aguardando lead ligar).
func isArmedStep(step string) bool {
	return strings.HasPrefix(step, "call_armed") ||
		strings.HasPrefix(step, "delivery_call") ||
		step == stepUpsellDeliveryArmed ||
		step == stepCallRevival
}

// isPaymentStep — retorna true se o step está aguardando pagamento (polling ativo).
func isPaymentStep(step string) bool {
	switch step {
	case stepPixSent, stepPixSent2, stepPixSent2Fu, stepUpsellPixSent, stepUpsellPixSentFu:
		return true
	}
	return false
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

	// Cancela timers pendentes (o lead respondeu) — exceto para steps com chamada
	// armada ou pagamento pendente, onde o re-arm/polling precisa sobreviver.
	if !isArmedStep(lead.Step) && !isPaymentStep(lead.Step) {
		_ = e.db.CancelActions(ctx, lead.ID)
	}

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

	case stepNew: // TESTE: pula direto pro envio de PIX + polling
		e.sendPixSequence(ctx, lead)

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

	case stepCallRevival: // segunda chance armada
		log.Printf("[engine] lead %d mandou msg com revival armada (step=call_revival)", lead.ID)

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

	case stepPixSent: // aguardando pagamento (1° PIX)
		log.Printf("[engine] lead %d mandou msg enquanto aguarda pagamento (step=pix_sent)", lead.ID)

	case stepPixSent2: // aguardando pagamento (2° PIX)
		log.Printf("[engine] lead %d mandou msg enquanto aguarda pagamento retry (step=pix_sent_2)", lead.ID)

	case stepPixSent2Fu: // aguardando pagamento (2° PIX, pós follow-up)
		log.Printf("[engine] lead %d mandou msg enquanto aguarda pagamento retry fu (step=pix_sent_2_fu)", lead.ID)

	case stepPixExpired: // 2° PIX expirou — lead perdido
		log.Printf("[engine] lead %d em pix_expired (lead perdido)", lead.ID)

	case stepDeliveryCallArmed, stepDeliveryCallArmed2: // chamada de entrega armada
		log.Printf("[engine] lead %d mandou msg com delivery call armada (step=%s)", lead.ID, lead.Step)

	case stepDeliveryGiveUp: // não ligou pra entrega
		log.Printf("[engine] lead %d em delivery_give_up", lead.ID)

	case stepUpsellPixSent: // aguardando upsell PIX
		log.Printf("[engine] lead %d mandou msg enquanto aguarda upsell pix", lead.ID)

	case stepUpsellPixSentFu: // upsell PIX follow-up enviado
		log.Printf("[engine] lead %d mandou msg enquanto aguarda upsell pix (pós follow-up)", lead.ID)

	case stepUpsellDeliveryArmed: // chamada entrega2 armada
		log.Printf("[engine] lead %d mandou msg com upsell delivery armada", lead.ID)

	case stepDone: // funil completo
		log.Printf("[engine] lead %d em done (funil completo)", lead.ID)

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

	case stepPixSent, stepPixSent2, stepPixSent2Fu, stepUpsellPixSent, stepUpsellPixSentFu:
		// Polling de pagamento
		if a.Kind == "payment_check" {
			e.checkPayment(ctx, lead, a)
		}

	default:
		// Re-arm de chamada para steps armados (dispara após chamada de outro lead)
		if a.Kind == "rearm_call" && isArmedStep(lead.Step) {
			if v, ok := a.Payload["video"].(string); ok {
				log.Printf("[engine] re-arm chamada para lead %d (step=%s)", lead.ID, lead.Step)
				e.armVideoCall(ctx, lead, v)
			}
		} else {
			log.Printf("[engine] timer %q não tratado para step %q (lead %d)", a.Kind, lead.Step, lead.ID)
		}
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

// send — adquire o gate da sessão e envia o texto com "digitando..." nativo.
// O delay é calculado pelo tamanho do texto (base + por-char, com teto de 15s).
// O wa-js mostra "digitando..." pelo tempo configurado e depois envia — tudo numa chamada.
func (e *Engine) send(ctx context.Context, lead *Lead, text string) error {
	e.gate.Acquire(lead.SessionID, lead.Phone)

	delaySec := int(typingFor(text).Seconds())
	if delaySec < 1 {
		delaySec = 1
	}
	if err := e.api.SendText(ctx, lead.SessionID, lead.Phone, text, delaySec); err != nil {
		e.gate.Done(lead.SessionID, lead.Phone)
		log.Printf("[engine] send text lead %d: %v", lead.ID, err)
		return err
	}
	_ = e.db.InsertMessage(ctx, lead.ID, "outbound", text, "text", "", lead.SessionID)
	e.db.LogEvent(ctx, lead.ID, "outbound", map[string]any{"body": text})

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
// Registra no CallTracker e mergeia allowed_numbers com outros leads armados
// que usam o mesmo vídeo na mesma sessão.
func (e *Engine) armVideoCall(ctx context.Context, lead *Lead, videoURL string) {
	// Registra no tracker
	e.calls.Add(lead.SessionID, ArmedLead{
		Phone: lead.Phone, LeadID: lead.ID, VideoURL: videoURL,
	})

	// Merge: todos os phones armados com o mesmo vídeo nessa sessão
	phones := e.calls.PhonesWithSameVideo(lead.SessionID, videoURL)
	allowedNumbers := strings.Join(phones, ",")

	if err := e.api.AcceptVideo(ctx, lead.SessionID, videoURL, allowedNumbers); err != nil {
		log.Printf("[engine] arm video call lead %d: %v", lead.ID, err)
		return
	}
	log.Printf("[engine] vídeo-chamada armada para lead %d (allowed=%s)", lead.ID, allowedNumbers)
	e.db.LogEvent(ctx, lead.ID, "outbound", map[string]any{
		"type": "arm_video_call", "video": videoURL, "allowed": allowedNumbers,
	})
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

	// Gera cobrança dinâmica no gateway (valor aleatório 29.01–29.99)
	if e.sendDynamicPix(ctx, lead) != nil {
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
	// Inicia polling de pagamento a cada ~20s
	_ = e.db.ScheduleAction(ctx, lead.ID, "payment_check", time.Now().Add(30*time.Second), nil)
}

// sendDynamicPix — gera PIX local com centavos únicos (range 29.01–30.99) e envia.
func (e *Engine) sendDynamicPix(ctx context.Context, lead *Lead) error {
	return e.sendDynamicPixAmount(ctx, lead, 29.01, 30.99)
}

// sendDynamicPixAmount — gera PIX EMV local com centavos únicos e envia.
// Aloca o próximo centavo livre dentro do range nos últimos 29 min.
func (e *Engine) sendDynamicPixAmount(ctx context.Context, lead *Lead, minAmount, maxAmount float64) error {
	minCents := int(minAmount * 100)
	maxCents := int(maxAmount * 100)

	// Busca centavos já usados nos últimos 29 min
	usedCents, _ := e.db.GetUsedCentsInRange(ctx, minCents, maxCents, 29)
	usedSet := make(map[int]bool, len(usedCents))
	for _, c := range usedCents {
		usedSet[c] = true
	}

	// Pega o próximo livre
	amountCents := minCents
	for c := minCents; c <= maxCents; c++ {
		if !usedSet[c] {
			amountCents = c
			break
		}
	}
	amount := float64(amountCents) / 100.0

	// Gera o código EMV localmente
	emv := generatePixEMV(pixChave, pixRecebedor, pixCidade, lead.Phone, amount)
	log.Printf("[engine] PIX gerado: lead=%d amount=%.2f txid=%s", lead.ID, amount, lead.Phone)

	// Salva no banco
	_ = e.db.InsertPayment(ctx, lead.ID, "pix_local", lead.Phone, amountCents, emv)

	// Envia via WhatsApp (sendPixKeyMessage com EVP + código EMV como key)
	e.gate.Acquire(lead.SessionID, lead.Phone)
	e.typing(ctx, lead, cfgTypingBase)
	if err := e.api.SendPix(ctx, lead.SessionID, lead.Phone, pixKeyType, pixName, emv, ""); err != nil {
		e.gate.Done(lead.SessionID, lead.Phone)
		log.Printf("[engine] send pix lead %d: %v", lead.ID, err)
		return err
	}
	_ = e.db.InsertMessage(ctx, lead.ID, "outbound", "[pix]", "pix", "", lead.SessionID)
	e.db.LogEvent(ctx, lead.ID, "outbound", map[string]any{"type": "pix", "amount": amount, "emv": emv})
	e.gate.Done(lead.SessionID, lead.Phone)
	return nil
}

// checkPayment — consulta o status do pagamento via webhook.
func (e *Engine) checkPayment(ctx context.Context, lead *Lead, a *Action) {
	pp, err := e.db.GetPendingPaymentInfo(ctx, lead.ID)
	if err != nil {
		log.Printf("[engine] get pending payment lead %d: %v", lead.ID, err)
		return
	}

	// Contador de checks (payload "n")
	checkCount := 0
	if a.Payload != nil {
		if n, ok := a.Payload["n"]; ok {
			if nf, ok := n.(float64); ok {
				checkCount = int(nf)
			}
		}
	}

	amount := float64(pp.AmountCents) / 100.0
	status, payerName, err := e.pay.CheckStatus(ctx, lead.Phone, amount, pp.CreatedAt)
	if err != nil {
		log.Printf("[engine] check payment lead %d: %v", lead.ID, err)
		_ = e.db.ScheduleAction(ctx, lead.ID, "payment_check", time.Now().Add(30*time.Second), map[string]any{"n": float64(checkCount)})
		return
	}

	log.Printf("[engine] payment check lead=%d amount=%.2f status=%s payer=%s count=%d step=%s", lead.ID, amount, status, payerName, checkCount, lead.Step)

	switch status {
	case "paid":
		_ = e.db.CancelActions(ctx, lead.ID)
		_ = e.db.UpdatePaymentStatusByID(ctx, pp.ID, "paid")
		e.db.LogEvent(ctx, lead.ID, "payment", map[string]any{"status": "paid", "amount": amount, "payer": payerName})
		log.Printf("[engine] \xf0\x9f\x92\xb0 lead %d PAGOU! amount=%.2f", lead.ID, amount)
		if lead.Step == stepUpsellPixSent {
			e.sendUpsellPaidSequence(ctx, lead)
		} else {
			e.sendPaidSequence(ctx, lead, payerName)
		}

	case "pending":
		nextCount := checkCount + 1

		if lead.Step == stepPixSent2 && nextCount >= 20 {
			e.sendPixRetryFollowUp(ctx, lead)
		}

		if lead.Step == stepUpsellPixSent && nextCount >= 20 {
			e.send(ctx, lead, msgUpFu)
			e.goTo(ctx, lead, "awaiting_payment", stepUpsellPixSentFu)
		}

		_ = e.db.ScheduleAction(ctx, lead.ID, "payment_check", time.Now().Add(30*time.Second), map[string]any{"n": float64(nextCount)})

	case "expired":
		_ = e.db.UpdatePaymentStatusByID(ctx, pp.ID, "expired")
		e.db.LogEvent(ctx, lead.ID, "payment", map[string]any{"status": "expired", "amount": amount})

		switch lead.Step {
		case stepPixSent:
			log.Printf("[engine] lead %d PIX expirou — enviando retry", lead.ID)
			e.sendPixRetrySequence(ctx, lead)
		case stepPixSent2, stepPixSent2Fu:
			log.Printf("[engine] lead %d 2° PIX expirou — desistindo", lead.ID)
			e.goTo(ctx, lead, "lost", stepPixExpired)
		case stepUpsellPixSent:
			log.Printf("[engine] lead %d upsell PIX expirou", lead.ID)
			e.goTo(ctx, lead, "paid", stepDone)
		}
	}
}

// sendPixRetrySequence \xe2\x80\x94 1\xc2\xb0 PIX expirou \xe2\x86\x92 mensagens + \xc3\xa1udios + novo PIX menor.
func (e *Engine) sendPixRetrySequence(ctx context.Context, lead *Lead) {
	if e.send(ctx, lead, msgPixRetryA) != nil { return }
	time.Sleep(5 * time.Second)
	if e.send(ctx, lead, msgPixRetryB) != nil { return }
	time.Sleep(20 * time.Second)
	if e.send(ctx, lead, msgPixRetryC) != nil { return }
	time.Sleep(5 * time.Second)
	if e.send(ctx, lead, msgPixRetryD) != nil { return }
	time.Sleep(90 * time.Second)
	if e.sendAudioURL(ctx, lead, audioYas6) != nil { return }
	time.Sleep(5 * time.Second)
	if e.sendAudioURL(ctx, lead, audioYas7) != nil { return }
	time.Sleep(6 * time.Second)
	if e.sendAudioURL(ctx, lead, audioYas8) != nil { return }
	time.Sleep(10 * time.Second)
	if e.send(ctx, lead, msgPixRetryE) != nil { return }
	time.Sleep(31 * time.Second)

	// Gera novo PIX com valor menor (20.01\xe2\x80\x9320.99)
	if e.sendDynamicPixAmount(ctx, lead, 19.01, 20.99) != nil { return }

	time.Sleep(22 * time.Second)
	if e.send(ctx, lead, msgPixRetryF) != nil { return }

	e.goTo(ctx, lead, "awaiting_payment", stepPixSent2)
	_ = e.db.ScheduleAction(ctx, lead.ID, "payment_check", time.Now().Add(30*time.Second), map[string]any{"n": float64(0)})
}

// sendPixRetryFollowUp \xe2\x80\x94 2\xc2\xb0 PIX pendente h\xc3\xa1 10 min \xe2\x86\x92 fotos + insist\xc3\xaancia.
func (e *Engine) sendPixRetryFollowUp(ctx context.Context, lead *Lead) {
	if e.send(ctx, lead, msgPixFuA) != nil { return }
	time.Sleep(5 * time.Second)
	if e.send(ctx, lead, msgPixFuB) != nil { return }
	time.Sleep(10 * time.Second)
	if e.send(ctx, lead, msgPixFuC) != nil { return }
	time.Sleep(5 * time.Second)
	if e.sendImageURL(ctx, lead, imgFotoB1, "", false) != nil { return }
	time.Sleep(3 * time.Second)
	if e.sendImageURL(ctx, lead, imgFotoB2, "", false) != nil { return }
	time.Sleep(15 * time.Second)
	if e.send(ctx, lead, msgPixFuD) != nil { return }
	time.Sleep(5 * time.Second)
	if e.send(ctx, lead, msgPixFuE) != nil { return }
	e.goTo(ctx, lead, "awaiting_payment", stepPixSent2Fu)
}

// ── Compra aprovada + entrega + upsell ──────────────────────────────────────

// sendPaidSequence — sequência completa pós-pagamento: agradecimento + link + chamada de entrega.
func (e *Engine) sendPaidSequence(ctx context.Context, lead *Lead, payerName string) {
	e.goTo(ctx, lead, "paid", "paid")
	if e.send(ctx, lead, msgPd01) != nil { return }
	time.Sleep(5 * time.Second)
	if e.send(ctx, lead, msgPd02) != nil { return }
	time.Sleep(15 * time.Second)
	if payerName != "" {
		if e.send(ctx, lead, payerName+" né?") != nil { return }
		time.Sleep(5 * time.Second)
	}
	if e.send(ctx, lead, msgPd03) != nil { return }
	time.Sleep(10 * time.Second)
	if e.send(ctx, lead, msgPd04) != nil { return }
	time.Sleep(10 * time.Second)
	if e.send(ctx, lead, msgPd05) != nil { return }
	time.Sleep(10 * time.Second)
	if e.send(ctx, lead, msgPd06) != nil { return }
	time.Sleep(10 * time.Second)
	if e.send(ctx, lead, msgPd07) != nil { return }
	time.Sleep(5 * time.Second)
	if e.send(ctx, lead, msgPd08) != nil { return }
	time.Sleep(15 * time.Second)
	if e.send(ctx, lead, msgPd09) != nil { return }
	time.Sleep(8 * time.Second)
	if e.send(ctx, lead, msgPd10) != nil { return }
	time.Sleep(5 * time.Second)
	if e.send(ctx, lead, msgPd11) != nil { return }
	time.Sleep(5 * time.Second)
	if e.send(ctx, lead, msgPd12) != nil { return }
	time.Sleep(10 * time.Second)
	if e.send(ctx, lead, msgPd13) != nil { return }
	time.Sleep(28 * time.Second)
	if e.send(ctx, lead, msgPd14) != nil { return }
	time.Sleep(5 * time.Second)
	if e.send(ctx, lead, msgPd15) != nil { return }
	time.Sleep(6 * time.Second)
	if e.send(ctx, lead, msgPd16) != nil { return }
	time.Sleep(3 * time.Second)
	if e.send(ctx, lead, msgPd17) != nil { return }
	time.Sleep(3 * time.Second)
	if e.send(ctx, lead, msgPd18) != nil { return }
	time.Sleep(16 * time.Second)
	if e.send(ctx, lead, msgPd19) != nil { return }
	time.Sleep(3 * time.Second)
	if e.send(ctx, lead, msgPd20) != nil { return }
	time.Sleep(9 * time.Second)
	if e.send(ctx, lead, msgPd21) != nil { return }
	time.Sleep(18 * time.Second)
	if e.send(ctx, lead, msgPd22) != nil { return }
	time.Sleep(90 * time.Second)
	if e.send(ctx, lead, msgPd23) != nil { return }
	time.Sleep(11 * time.Second)
	if e.send(ctx, lead, msgPd24) != nil { return }
	time.Sleep(11 * time.Second)
	if e.send(ctx, lead, msgPd25) != nil { return }
	time.Sleep(6 * time.Second)
	if e.send(ctx, lead, msgPd26) != nil { return }
	time.Sleep(12 * time.Second)
	if e.send(ctx, lead, msgPd27) != nil { return }
	time.Sleep(6 * time.Second)
	if e.send(ctx, lead, msgPd28) != nil { return }
	time.Sleep(28 * time.Second)
	if e.send(ctx, lead, msgPd29) != nil { return }

	// Arma chamada de vídeo de entrega
	e.armVideoCall(ctx, lead, videoEntrega1)
	time.Sleep(12 * time.Second)
	if e.send(ctx, lead, msgPd30) != nil { return }
	time.Sleep(5 * time.Second)
	if e.send(ctx, lead, msgPd31) != nil { return }
	time.Sleep(5 * time.Second)
	if e.send(ctx, lead, msgPd32) != nil { return }
	time.Sleep(5 * time.Second)
	if e.send(ctx, lead, msgPd33) != nil { return }
	time.Sleep(5 * time.Second)
	if e.send(ctx, lead, msgPd34) != nil { return }
	time.Sleep(12 * time.Second)
	if e.send(ctx, lead, msgPd35) != nil { return }
	e.goTo(ctx, lead, "paid", stepDeliveryCallArmed)
}

// sendPostDeliverySequence — pós-chamada de entrega: espera 5min + upsell.
func (e *Engine) sendPostDeliverySequence(ctx context.Context, lead *Lead) {
	// Espera o vídeo terminar (~5 min ou duração do vídeo)
	time.Sleep(5 * time.Minute)
	time.Sleep(30 * time.Second)
	if e.send(ctx, lead, msgUp01) != nil { return }
	time.Sleep(30 * time.Second)
	if e.send(ctx, lead, msgUp02) != nil { return }
	time.Sleep(6 * time.Second)
	if e.send(ctx, lead, msgUp03) != nil { return }
	time.Sleep(26 * time.Second)
	if e.send(ctx, lead, msgUp04) != nil { return }
	time.Sleep(6 * time.Second)
	if e.send(ctx, lead, msgUp05) != nil { return }
	time.Sleep(6 * time.Second)
	if e.send(ctx, lead, msgUp06) != nil { return }
	time.Sleep(3 * time.Second)
	if e.send(ctx, lead, msgUp07) != nil { return }
	time.Sleep(5 * time.Second)
	if e.sendImageURL(ctx, lead, imgUpsellApp, "", false) != nil { return }
	time.Sleep(10 * time.Second)
	if e.send(ctx, lead, msgUp08) != nil { return }
	time.Sleep(8 * time.Second)
	if e.send(ctx, lead, msgUp09) != nil { return }
	time.Sleep(5 * time.Second)
	if e.send(ctx, lead, msgUp10) != nil { return }
	time.Sleep(11 * time.Second)
	if e.send(ctx, lead, msgUp11) != nil { return }
	time.Sleep(5 * time.Second)
	if e.send(ctx, lead, msgUp12) != nil { return }
	time.Sleep(10 * time.Second)

	// Envia PIX upsell (R$14.19–14.99)
	if e.sendDynamicPixAmount(ctx, lead, 14.99, 15.99) != nil { return }
	e.goTo(ctx, lead, "awaiting_payment", stepUpsellPixSent)
	_ = e.db.ScheduleAction(ctx, lead.ID, "payment_check", time.Now().Add(30*time.Second), map[string]any{"n": float64(0)})
}

// sendDeliveryCallFollowUp1 — 1ª vez que não ligou pra entrega.
func (e *Engine) sendDeliveryCallFollowUp1(ctx context.Context, lead *Lead) {
	if e.send(ctx, lead, msgDcf1a) != nil { return }
	time.Sleep(10 * time.Second)
	if e.send(ctx, lead, msgDcf1b) != nil { return }
	time.Sleep(5 * time.Second)
	if e.send(ctx, lead, msgDcf1c) != nil { return }
	time.Sleep(3 * time.Second)
	if e.send(ctx, lead, msgDcf1d) != nil { return }
	// Re-arma chamada de entrega
	e.armVideoCall(ctx, lead, videoEntrega1)
	time.Sleep(10 * time.Second)
	if e.send(ctx, lead, msgDcf1e) != nil { return }
	time.Sleep(5 * time.Second)
	if e.send(ctx, lead, msgDcf1f) != nil { return }
	e.goTo(ctx, lead, "paid", stepDeliveryCallArmed2)
}

// sendDeliveryGiveUp — 2ª vez que não ligou pra entrega → despedida.
func (e *Engine) sendDeliveryGiveUp(ctx context.Context, lead *Lead) {
	if e.send(ctx, lead, msgDcf2a) != nil { return }
	time.Sleep(5 * time.Second)
	if e.send(ctx, lead, msgDcf2b) != nil { return }
	time.Sleep(5 * time.Second)
	if e.send(ctx, lead, msgDcf2c) != nil { return }
	time.Sleep(5 * time.Second)
	if e.send(ctx, lead, msgDcf2d) != nil { return }
	time.Sleep(5 * time.Second)
	if e.send(ctx, lead, msgDcf2e) != nil { return }
	e.goTo(ctx, lead, "paid", stepDeliveryGiveUp)
}

// sendUpsellPaidSequence — upsell PIX pago → agradece + arma entrega2.
func (e *Engine) sendUpsellPaidSequence(ctx context.Context, lead *Lead) {
	e.goTo(ctx, lead, "paid", "upsell_paid")
	if e.send(ctx, lead, msgUpPd1) != nil { return }
	time.Sleep(5 * time.Second)
	if e.send(ctx, lead, msgUpPd2) != nil { return }
	// Arma chamada de entrega 2
	e.armVideoCall(ctx, lead, videoEntrega2)
	time.Sleep(5 * time.Second)
	if e.send(ctx, lead, msgUpPd3) != nil { return }
	time.Sleep(5 * time.Second)
	if e.send(ctx, lead, msgUpPd4) != nil { return }
	time.Sleep(5 * time.Second)
	if e.send(ctx, lead, msgUpPd5) != nil { return }
	e.goTo(ctx, lead, "paid", stepUpsellDeliveryArmed)
}


// HandleCallEvent — processa eventos de chamada (aceita, expirada, rejeitada).
func (e *Engine) HandleCallEvent(ctx context.Context, ev *CallEventJob) {
	var lead *Lead
	var err error

	// Todos os steps possíveis com chamada armada
	allArmedSteps := []string{
		stepCallArmed, stepCallArmed2, stepCallArmed3, stepCallArmed4,
		stepDeliveryCallArmed, stepDeliveryCallArmed2,
		stepUpsellDeliveryArmed, stepCallRevival,
	}

	if ev.Phone != "" {
		lead, err = e.db.GetLeadByPhone(ctx, ev.Phone, ev.SessionID)
	} else {
		// Expired: busca o lead em qualquer step armado
		for _, step := range allArmedSteps {
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
		// Remove o lead aceito do tracker e notifica os outros
		e.calls.Remove(ev.SessionID, lead.Phone)
		others := e.calls.GetAll(ev.SessionID)
		if len(others) > 0 {
			e.notifyAndRearmOthers(ctx, ev.SessionID, others)
		}

		switch {
		case strings.HasPrefix(lead.Step, "call_armed"):
			log.Printf("[engine] lead %d ligou (step=%s) — continuando copy", lead.ID, lead.Step)
			e.sendPostCallSequence(ctx, lead)
		case strings.HasPrefix(lead.Step, "delivery_call"):
			log.Printf("[engine] lead %d ligou pra entrega (step=%s) — upsell", lead.ID, lead.Step)
			e.sendPostDeliverySequence(ctx, lead)
		case lead.Step == stepUpsellDeliveryArmed:
			log.Printf("[engine] lead %d ligou pra entrega2 (upsell) — done", lead.ID)
			e.goTo(ctx, lead, "paid", stepDone)
		case lead.Step == stepCallRevival:
			// Segunda chance — lead ligou depois de ter desistido
			log.Printf("[engine] lead %d ligou na segunda chance (revival) — continuando copy", lead.ID)
			e.sendPostCallSequence(ctx, lead)
		}

	case "expired":
		// Remove do tracker
		e.calls.Remove(ev.SessionID, lead.Phone)

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
		case stepDeliveryCallArmed:
			log.Printf("[engine] lead %d não ligou pra entrega (tentativa 1)", lead.ID)
			e.sendDeliveryCallFollowUp1(ctx, lead)
		case stepDeliveryCallArmed2:
			log.Printf("[engine] lead %d não ligou pra entrega (tentativa 2) — desistindo", lead.ID)
			e.sendDeliveryGiveUp(ctx, lead)
		case stepUpsellDeliveryArmed:
			log.Printf("[engine] lead %d não ligou pra entrega2 (upsell) — done", lead.ID)
			e.goTo(ctx, lead, "paid", stepDone)
		case stepCallRevival:
			// Segunda chance expirou — volta pro give up
			log.Printf("[engine] lead %d não ligou na segunda chance — volta pra give_up", lead.ID)
			e.goTo(ctx, lead, "lost", stepCallGiveUp)
		}

	case "rejected":
		// Chamada rejeitada (número não estava no allowlist por race condition)
		if isArmedStep(lead.Step) {
			log.Printf("[engine] lead %d chamada rejeitada (step=%s) — perai + re-arm", lead.ID, lead.Step)
			e.send(ctx, lead, msgCallBusy)
			// Re-arma em 2 min (busca vídeo do tracker)
			armed := e.calls.GetAll(ev.SessionID)
			for _, al := range armed {
				if al.Phone == lead.Phone {
					_ = e.db.ScheduleAction(ctx, lead.ID, "rearm_call", time.Now().Add(1*time.Minute),
						map[string]any{"video": al.VideoURL})
					break
				}
			}
		}

	case "incoming":
		// Chamada recebida (antes de aceitar) — cenários avulsos
		e.handleIncomingCall(ctx, lead)
	}
}

// notifyAndRearmOthers — quando um lead é aceito, avisa os outros armados na mesma
// sessão ("perai") e agenda re-arm em 2 min (após a chamada ativa terminar).
func (e *Engine) notifyAndRearmOthers(ctx context.Context, sessionID string, others []ArmedLead) {
	for _, al := range others {
		go func(al ArmedLead) {
			otherLead, err := e.db.GetLead(ctx, al.LeadID)
			if err != nil {
				log.Printf("[engine] notify other lead %d: %v", al.LeadID, err)
				return
			}
			log.Printf("[engine] notificando lead %d (sessão ocupada) — re-arm em 2 min", al.LeadID)
			e.send(ctx, otherLead, msgCallBusy)
			_ = e.db.ScheduleAction(ctx, al.LeadID, "rearm_call", time.Now().Add(1*time.Minute),
				map[string]any{"video": al.VideoURL})
		}(al)
	}
}

// handleIncomingCall — trata chamada recebida pra cenários avulsos:
// 1) Lead pós-chamada/pré-PIX insistindo em ligar → aviso (1x)
// 2) Lead em give-up que ligou → segunda chance (1x)
func (e *Engine) handleIncomingCall(ctx context.Context, lead *Lead) {
	switch lead.Step {
	case stepAwaitQ5, stepAwaitQ6:
		// Pós-chamada, pré-PIX: lead insiste em ligar
		key := fmt.Sprintf("call_warn:%d", lead.ID)
		if _, loaded := e.once.LoadOrStore(key, true); !loaded {
			log.Printf("[engine] lead %d ligando pós-chamada/pré-PIX — aviso (1x)", lead.ID)
			e.sendCallInsistWarning(ctx, lead)
		}

	case stepCallGiveUp:
		// Lead desistiu mas agora ligou — segunda chance
		key := fmt.Sprintf("call_revival:%d", lead.ID)
		if _, loaded := e.once.LoadOrStore(key, true); !loaded {
			log.Printf("[engine] lead %d ligou depois de give-up — segunda chance", lead.ID)
			e.sendCallGiveUpRevival(ctx, lead)
		}
	}
}

// sendCallInsistWarning — "amooor, não precisa ligar" (1x, pós-chamada/pré-PIX).
func (e *Engine) sendCallInsistWarning(ctx context.Context, lead *Lead) {
	if e.send(ctx, lead, msgInsist1) != nil { return }
	time.Sleep(3 * time.Second)
	if e.send(ctx, lead, msgInsist2) != nil { return }
	time.Sleep(3 * time.Second)
	if e.send(ctx, lead, msgInsist3) != nil { return }
	time.Sleep(3 * time.Second)
	if e.send(ctx, lead, msgInsist4) != nil { return }
	time.Sleep(3 * time.Second)
	if e.send(ctx, lead, msgInsist5) != nil { return }
	// Lead continua no mesmo step — a copy segue normalmente após 20s
}

// sendCallGiveUpRevival — "vi que me ligou" → arma accept → segunda chance.
func (e *Engine) sendCallGiveUpRevival(ctx context.Context, lead *Lead) {
	if e.send(ctx, lead, msgRevival1) != nil { return }
	time.Sleep(5 * time.Second)
	if e.send(ctx, lead, msgRevival2) != nil { return }
	time.Sleep(5 * time.Second)
	if e.send(ctx, lead, msgRevival3) != nil { return }
	time.Sleep(5 * time.Second)
	if e.send(ctx, lead, msgRevival4) != nil { return }
	// Arma video-chamada — segunda chance
	e.armVideoCall(ctx, lead, videoCall1)
	e.goTo(ctx, lead, "in_flow", stepCallRevival)
}
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
