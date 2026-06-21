package main

import (
	"context"
	"log"
)

// CallEventJob — evento de chamada (aceita, expirada, etc).
type CallEventJob struct {
	Phone     string // vazio para "expired" (resolve via DB)
	SessionID string
	Event     string // "accepted" ou "expired"
}

// Job — uma unidade de trabalho (mensagem recebida, timer ou evento de chamada).
type Job struct {
	Inbound   *InboundJob
	Timer     *Action
	CallEvent *CallEventJob
}

// Queue — processamento POR LEAD (Opção C). Cada job roda numa goroutine que
// trava o lock do lead: leads diferentes em paralelo, mesmo lead serial.
// Isso permite os delays humanos (30-90s) sem travar os outros leads — e
// continua sem duplicar (lock por lead + a constraint UNIQUE no banco).
type Queue struct {
	eng *Engine
	km  *KeyedMutex
}

func NewQueue(eng *Engine) *Queue {
	return &Queue{eng: eng, km: NewKeyedMutex()}
}

// Enqueue — dispara o processamento do job na goroutine do seu lead.
func (q *Queue) Enqueue(job Job) {
	key := jobKey(job)
	if key == "" {
		return
	}
	go func() {
		q.km.Lock(key)
		defer q.km.Unlock(key)
		q.process(job)
	}()
}

// jobKey — a chave de serialização é sempre o telefone do lead.
func jobKey(job Job) string {
	switch {
	case job.Inbound != nil:
		return job.Inbound.Phone
	case job.Timer != nil:
		return job.Timer.Phone
	case job.CallEvent != nil:
		if job.CallEvent.Phone != "" {
			return job.CallEvent.Phone
		}
		return "session:" + job.CallEvent.SessionID
	}
	return ""
}

func (q *Queue) process(job Job) {
	ctx := context.Background()
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[queue] panic ao processar job: %v", r)
			// Safety net: release the send gate if the goroutine panicked
			// between Acquire and Done — otherwise the session stays locked.
			if job.Inbound != nil {
				q.eng.gate.ForceRelease(job.Inbound.SessionID, job.Inbound.Phone)
			}
		}
	}()
	switch {
	case job.Inbound != nil:
		q.eng.HandleInbound(ctx, job.Inbound)
	case job.Timer != nil:
		q.eng.HandleTimer(ctx, job.Timer)
	case job.CallEvent != nil:
		q.eng.HandleCallEvent(ctx, job.CallEvent)
	}
}
