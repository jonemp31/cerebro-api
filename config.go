package main

import (
	"log"
	"math/rand"
	"os"
	"time"
)

func envDur(key string, def time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}

// Humanização — digitação (configurável por env).
var (
	cfgTypingBase    = envDur("TYPING_BASE", 3*time.Second)
	cfgTypingPerChar = envDur("TYPING_PER_CHAR", 60*time.Millisecond)
	cfgTypingCap     = envDur("TYPING_CAP", 15*time.Second)
)

// ── Delay de leitura dinâmico por horário (America/Sao_Paulo) ────────────────
// Simula o comportamento de um humano real ao longo do dia: responde mais rápido
// quando está alerta, mais devagar quando está comendo, dormindo ou acordando.

var brLoc = func() *time.Location {
	loc, err := time.LoadLocation("America/Sao_Paulo")
	if err != nil {
		log.Printf("[config] AVISO: timezone America/Sao_Paulo não encontrada, usando UTC")
		return time.UTC
	}
	return loc
}()

type delaySlot struct {
	startHour int
	endHour   int           // inclusivo (ex: 7 = até 07:59)
	min       time.Duration
	max       time.Duration
	label     string
}

// Horários e delays — edite aqui para ajustar o "ritmo" do bot.
var delaySchedule = []delaySlot{
	{0, 1, 30 * time.Second, 60 * time.Second, "sono leve"},
	{2, 3, 30 * time.Second, 90 * time.Second, "bastante sono"},
	{4, 5, 40 * time.Second, 100 * time.Second, "muito sono"},
	{6, 7, 25 * time.Second, 45 * time.Second, "acordando"},
	{8, 10, 15 * time.Second, 35 * time.Second, "trabalho"},
	{11, 13, 20 * time.Second, 50 * time.Second, "almoço"},
	{14, 17, 15 * time.Second, 25 * time.Second, "energia"},
	{18, 19, 20 * time.Second, 55 * time.Second, "janta"},
	{20, 23, 15 * time.Second, 25 * time.Second, "atento"},
}

// replyDelayRange retorna (min, max) baseado na hora atual em SP.
func replyDelayRange() (min, max time.Duration, label string) {
	h := time.Now().In(brLoc).Hour()
	for _, s := range delaySchedule {
		if h >= s.startHour && h <= s.endHour {
			return s.min, s.max, s.label
		}
	}
	// fallback (todos os horários estão cobertos, mas por segurança)
	return 20 * time.Second, 40 * time.Second, "fallback"
}

// humanDelay calcula e aplica o delay de "leitura" baseado no horário.
func humanDelay() {
	min, max, label := replyDelayRange()
	d := min
	if max > min {
		d += time.Duration(rand.Int63n(int64(max - min)))
	}
	log.Printf("[humanize] faixa %q (%s–%s) → delay %.0fs", label, min, max, d.Seconds())
	time.Sleep(d)
}
