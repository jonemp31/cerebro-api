package main

import (
	"log"
	"math/rand"
	"strings"
	"sync"
	"time"
)

// BatchMsg — uma mensagem individual dentro de um lote debounced.
type BatchMsg struct {
	Body    string
	WAMsgID string
}

// Debouncer — agrupa mensagens rápidas do mesmo lead numa só.
//
// Quando um lead manda "oi" e 2 segundos depois "tudo bem?", sem debounce
// o funil avançaria 2 passos. Com debounce, abre uma janela de 8-12s:
// se mais mensagens chegam, reseta o timer. Quando a janela fecha, tudo
// é combinado num único InboundJob e enfileirado.
//
// Janela de 8-12s é invisível pro lead — o replyDelay já é de 15-100s.
type Debouncer struct {
	mu      sync.Mutex
	buffers map[string]*msgBuffer
	q       *Queue
	minWait time.Duration
	maxWait time.Duration
}

type msgBuffer struct {
	job   InboundJob      // metadados da primeira mensagem (phone, sessionID, name)
	parts []BatchMsg      // mensagens acumuladas
	seen  map[string]bool // WAMsgIDs já neste buffer (dedup de retries)
	timer *time.Timer
}

func NewDebouncer(q *Queue, minWait, maxWait time.Duration) *Debouncer {
	return &Debouncer{
		buffers: make(map[string]*msgBuffer),
		q:       q,
		minWait: minWait,
		maxWait: maxWait,
	}
}

// Add — adiciona uma mensagem ao buffer do lead. Se é a primeira, abre a janela.
// Se já existe buffer, acumula e reseta o timer.
func (d *Debouncer) Add(j InboundJob) {
	key := j.Phone + "|" + j.SessionID

	d.mu.Lock()
	defer d.mu.Unlock()

	buf, exists := d.buffers[key]

	if !exists {
		buf = &msgBuffer{
			job:  j,
			seen: make(map[string]bool),
		}
		d.buffers[key] = buf
	}

	// Dedup: webhook retries que chegam durante a janela
	if j.WAMsgID != "" && buf.seen[j.WAMsgID] {
		return
	}

	buf.parts = append(buf.parts, BatchMsg{Body: j.Body, WAMsgID: j.WAMsgID})
	if j.WAMsgID != "" {
		buf.seen[j.WAMsgID] = true
	}

	// Atualiza o nome se a primeira mensagem não tinha
	if buf.job.Name == "" && j.Name != "" {
		buf.job.Name = j.Name
	}

	// Reseta o timer (nova janela a cada mensagem)
	if buf.timer != nil {
		buf.timer.Stop()
	}
	wait := d.randomWait()
	buf.timer = time.AfterFunc(wait, func() { d.flush(key) })

	if len(buf.parts) > 1 {
		log.Printf("[debounce] lead %s: %d msgs acumuladas (janela %.0fs)", j.Phone, len(buf.parts), wait.Seconds())
	}
}

// flush — janela fechou. Combina as mensagens e enfileira um único job.
func (d *Debouncer) flush(key string) {
	d.mu.Lock()
	buf, ok := d.buffers[key]
	if !ok {
		d.mu.Unlock()
		return
	}
	delete(d.buffers, key)
	d.mu.Unlock()

	// Monta o job combinado
	job := buf.job
	if len(buf.parts) == 1 {
		// Mensagem única: sem batching
		job.Body = buf.parts[0].Body
		job.WAMsgID = buf.parts[0].WAMsgID
		job.BatchMsgs = nil
	} else {
		// Múltiplas mensagens: combina os textos
		bodies := make([]string, len(buf.parts))
		for i, p := range buf.parts {
			bodies[i] = p.Body
		}
		job.Body = strings.Join(bodies, "\n")
		job.WAMsgID = buf.parts[0].WAMsgID // primeiro ID p/ dedup
		job.BatchMsgs = buf.parts
		log.Printf("[debounce] flush lead %s: %d msgs combinadas → %q", job.Phone, len(buf.parts), truncate(job.Body, 100))
	}

	d.q.Enqueue(Job{Inbound: &job})
}

func (d *Debouncer) randomWait() time.Duration {
	w := d.minWait
	if d.maxWait > d.minWait {
		w += time.Duration(rand.Int63n(int64(d.maxWait - d.minWait)))
	}
	return w
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
