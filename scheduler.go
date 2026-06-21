package main

import (
	"context"
	"log"
	"time"
)

// Scheduler — varre a tabela scheduled_actions e dispara o que venceu, jogando
// na MESMA fila do worker (mantém o processamento 1 a 1). Infra pronta para os
// follow-ups; na Fase 1 ainda não agendamos timers (o funil é movido por resposta).
type Scheduler struct {
	db *DB
	q  *Queue
}

func NewScheduler(db *DB, q *Queue) *Scheduler {
	return &Scheduler{db: db, q: q}
}

func (s *Scheduler) Start(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(20 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			actions, err := s.db.DueActions(ctx)
			if err != nil {
				log.Printf("[scheduler] due actions: %v", err)
				continue
			}
			for i := range actions {
				a := actions[i]
				s.q.Enqueue(Job{Timer: &a})
			}
		}
	}()
}
