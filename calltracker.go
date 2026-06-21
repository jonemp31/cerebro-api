package main

import "sync"

// ArmedLead — lead com chamada armada.
type ArmedLead struct {
	Phone    string
	LeadID   int64
	VideoURL string
}

// CallTracker — rastreia leads com chamada armada por sessão (in-memory).
// Permite merge de allowed_numbers e re-arm pós-aceite.
type CallTracker struct {
	mu    sync.Mutex
	armed map[string][]ArmedLead // sessionID → leads armados
}

func NewCallTracker() *CallTracker {
	return &CallTracker{armed: make(map[string][]ArmedLead)}
}

// Add — registra um lead como armado. Remove entrada anterior do mesmo phone.
func (ct *CallTracker) Add(sessionID string, al ArmedLead) {
	ct.mu.Lock()
	defer ct.mu.Unlock()
	leads := ct.armed[sessionID]
	filtered := make([]ArmedLead, 0, len(leads))
	for _, l := range leads {
		if l.Phone != al.Phone {
			filtered = append(filtered, l)
		}
	}
	ct.armed[sessionID] = append(filtered, al)
}

// Remove — remove um lead armado pelo phone.
func (ct *CallTracker) Remove(sessionID, phone string) {
	ct.mu.Lock()
	defer ct.mu.Unlock()
	leads := ct.armed[sessionID]
	filtered := make([]ArmedLead, 0, len(leads))
	for _, l := range leads {
		if l.Phone != phone {
			filtered = append(filtered, l)
		}
	}
	if len(filtered) == 0 {
		delete(ct.armed, sessionID)
	} else {
		ct.armed[sessionID] = filtered
	}
}

// GetAll — retorna todos os leads armados na sessão (cópia).
func (ct *CallTracker) GetAll(sessionID string) []ArmedLead {
	ct.mu.Lock()
	defer ct.mu.Unlock()
	return append([]ArmedLead{}, ct.armed[sessionID]...)
}

// PhonesWithSameVideo — retorna os phones de todos os leads armados com o mesmo vídeo.
func (ct *CallTracker) PhonesWithSameVideo(sessionID, videoURL string) []string {
	ct.mu.Lock()
	defer ct.mu.Unlock()
	var phones []string
	for _, l := range ct.armed[sessionID] {
		if l.VideoURL == videoURL {
			phones = append(phones, l.Phone)
		}
	}
	return phones
}
