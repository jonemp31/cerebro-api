package main

import (
	"context"
	"log"
)

// Job — uma unidade de trabalho na fila (mensagem recebida ou timer).
type Job struct {
	Inbound *InboundJob
	Timer   *Action
}

// Queue — fila EM PROCESSO, processada 1 a 1 por um único worker.
// Isso serializa tudo (evita corrida/duplicação) sem precisar de Redis/Rabbit
// nesse volume. Não-durável de propósito: a api faz retry de webhook e os
// timers ficam no banco, então um crash não perde estado.
type Queue struct {
	ch  chan Job
	eng *Engine
}

func NewQueue(eng *Engine, size int) *Queue {
	return &Queue{ch: make(chan Job, size), eng: eng}
}

func (q *Queue) Start() {
	go func() {
		for job := range q.ch {
			q.process(job)
		}
	}()
}

// Enqueue — coloca na fila. Bloqueia só se o buffer encher (raríssimo nesse volume).
func (q *Queue) Enqueue(job Job) {
	q.ch <- job
}

func (q *Queue) process(job Job) {
	ctx := context.Background()
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[queue] panic ao processar job: %v", r)
		}
	}()
	switch {
	case job.Inbound != nil:
		q.eng.HandleInbound(ctx, job.Inbound)
	case job.Timer != nil:
		q.eng.HandleTimer(ctx, job.Timer)
	}
}
