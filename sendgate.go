package main

import (
	"log"
	"sync"
	"time"
)

// SendGate serializes send operations per WhatsApp session.
//
// Multiple leads can "think" (read-delay) in parallel, but only ONE lead at a
// time can type+send on a given session — exactly like a real human who can
// read many chats but only type in one.
//
// After a send the gate stays held for `threshold` (default 60s) so the same
// lead can fire follow-up messages (e.g. text + pix) without another lead
// cutting in. Once the threshold expires with no new send, the gate opens.
//
// Re-entrant for the same lead: calling Acquire twice with the same phone just
// resets the auto-release timer.
type SendGate struct {
	threshold time.Duration

	mapMu sync.Mutex
	gates map[string]*gate
}

type gate struct {
	sendMu sync.Mutex // the exclusive send lock

	metaMu sync.Mutex  // protects holder + timer
	holder string      // phone of the lead currently holding the gate ("" = free)
	timer  *time.Timer // auto-release countdown
}

func NewSendGate(threshold time.Duration) *SendGate {
	return &SendGate{
		threshold: threshold,
		gates:     make(map[string]*gate),
	}
}

func (sg *SendGate) getGate(sessionID string) *gate {
	sg.mapMu.Lock()
	defer sg.mapMu.Unlock()
	g, ok := sg.gates[sessionID]
	if !ok {
		g = &gate{}
		sg.gates[sessionID] = g
	}
	return g
}

// Acquire blocks until the session's send slot is free, then claims it for phone.
//
// Re-entrant: if the same phone already holds the gate (follow-up send within
// the threshold window), the auto-release timer is cancelled and Acquire returns
// immediately — no blocking.
func (sg *SendGate) Acquire(sessionID, phone string) {
	g := sg.getGate(sessionID)

	// ── Fast path: same lead re-entering ─────────────────────────────────────
	g.metaMu.Lock()
	if g.holder == phone {
		if g.timer != nil {
			g.timer.Stop()
			g.timer = nil
		}
		g.metaMu.Unlock()
		log.Printf("[sendgate] re-entry %s on session %s (follow-up)", phone, sessionID)
		return
	}
	g.metaMu.Unlock()

	// ── Slow path: wait for the lock ─────────────────────────────────────────
	log.Printf("[sendgate] %s waiting for session %s", phone, sessionID)
	g.sendMu.Lock()

	g.metaMu.Lock()
	g.holder = phone
	if g.timer != nil {
		g.timer.Stop()
		g.timer = nil
	}
	g.metaMu.Unlock()
	log.Printf("[sendgate] %s acquired session %s", phone, sessionID)
}

// Done signals that a send finished. Instead of releasing immediately it starts
// a countdown of `threshold`. If Acquire from the same phone arrives before the
// timer fires the gate stays held (follow-up burst). Otherwise it opens.
func (sg *SendGate) Done(sessionID, phone string) {
	g := sg.getGate(sessionID)

	g.metaMu.Lock()
	defer g.metaMu.Unlock()

	if g.holder != phone {
		return // not the holder — noop
	}

	if g.timer != nil {
		g.timer.Stop()
	}
	g.timer = time.AfterFunc(sg.threshold, func() {
		g.metaMu.Lock()
		defer g.metaMu.Unlock()
		if g.holder == phone {
			g.holder = ""
			g.timer = nil
			g.sendMu.Unlock()
			log.Printf("[sendgate] %s released session %s (threshold expired)", phone, sessionID)
		}
	})
}

// ForceRelease immediately releases the gate for phone. Safety net for panics —
// if the processing goroutine crashes after Acquire but before Done, the gate
// would stay locked forever without this.
func (sg *SendGate) ForceRelease(sessionID, phone string) {
	g := sg.getGate(sessionID)

	g.metaMu.Lock()
	defer g.metaMu.Unlock()

	if g.holder != phone {
		return
	}
	if g.timer != nil {
		g.timer.Stop()
		g.timer = nil
	}
	g.holder = ""
	g.sendMu.Unlock()
	log.Printf("[sendgate] %s force-released session %s (panic recovery)", phone, sessionID)
}
