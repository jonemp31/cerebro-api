package main

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"
)

// Server — recebe os webhooks (da api-escala e, depois, do gateway) e enfileira.
type Server struct {
	debounce *Debouncer
	q        *Queue // eventos que não passam por debounce (chamadas, etc)
}

func NewServer(debounce *Debouncer, q *Queue) *Server {
	return &Server{debounce: debounce, q: q}
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/webhook/wa", s.handleWA)
	mux.HandleFunc("/webhook/pay", s.handlePay)
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	return mux
}

// Formato do webhook da api-escala: {"session_id":"...","data":{...evento...}}
type waWebhook struct {
	SessionID string `json:"session_id"`
	Data      struct {
		Event      string `json:"event"`
		FromNumber string `json:"from_number"`
		From       string `json:"from"`
		Body       string `json:"body"`
		MessageID  string `json:"messageId"`
		NotifyName string `json:"notifyName"`
		IsGroup    bool   `json:"isGroup"`
	} `json:"data"`
}

func (s *Server) handleWA(w http.ResponseWriter, r *http.Request) {
	// Responde 200 sempre que conseguir parsear — o trabalho real é assíncrono (fila).
	defer r.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))

	var p waWebhook
	if err := json.Unmarshal(raw, &p); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}

	// Só nos interessa uma mensagem de TEXTO de um lead (1:1, não grupo).
	// Mensagens de sistema/protocolo do WhatsApp (criptografia, etc.) chegam com
	// body vazio quando a conversa começa — IGNORAMOS, senão o funil avança sozinho.
	if p.Data.Event == "whatsapp_message_received" && !p.Data.IsGroup && strings.TrimSpace(p.Data.Body) != "" {
		phone := p.Data.FromNumber
		if phone == "" {
			phone = digits(p.Data.From)
		}
		if phone != "" {
			s.debounce.Add(InboundJob{
				Phone:     phone,
				SessionID: p.SessionID,
				Body:      p.Data.Body,
				WAMsgID:   p.Data.MessageID,
				Name:      p.Data.NotifyName,
			})
		}
	}

	// Eventos de chamada — vão direto pra fila (sem debounce).
	switch p.Data.Event {
	case "whatsapp_call_accepted":
		phone := p.Data.FromNumber
		if phone == "" {
			phone = digits(p.Data.From)
		}
		s.q.Enqueue(Job{CallEvent: &CallEventJob{
			Phone:     phone,
			SessionID: p.SessionID,
			Event:     "accepted",
		}})
	case "call_accept_expired":
		s.q.Enqueue(Job{CallEvent: &CallEventJob{
			SessionID: p.SessionID,
			Event:     "expired",
		}})
	}
	w.WriteHeader(http.StatusOK)
}

// handlePay — webhook do gateway de pagamento (Fase 1: stub; integramos quando você passar o gateway).
func (s *Server) handlePay(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	log.Printf("[webhook/pay] recebido (stub): %s", string(raw))
	w.WriteHeader(http.StatusOK)
}

// digits — mantém só os números (ex.: "5516999999999@c.us" -> "5516999999999").
func digits(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		if s[i] >= '0' && s[i] <= '9' {
			out = append(out, s[i])
		}
	}
	return string(out)
}
