package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"time"
)

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func main() {
	ctx := context.Background()

	dsn := env("DATABASE_URL", "postgres://cerebro:cerebro@localhost:5433/cerebro?sslmode=disable")
	apiURL := env("API_ESCALA_URL", "http://localhost:3000")
	addr := env("LISTEN_ADDR", ":8090")

	// Banco
	db, err := NewDB(ctx, dsn)
	if err != nil {
		log.Fatalf("[cerebro] db: %v", err)
	}
	defer db.Close()
	log.Println("[cerebro] postgres conectado")

	// Peças
	api := NewAPIClient(apiURL)         // fala com a api-escala (/send/*)
	gate := NewSendGate(envDur("SEND_GATE_THRESHOLD", 60*time.Second))
	eng := NewEngine(db, api, gate)     // a máquina de estados (o cérebro)
	q := NewQueue(eng)                  // fila por-lead (concorrente entre leads, serial por lead)
	debounce := NewDebouncer(q,          // agrupa msgs rápidas do mesmo lead (8-12s)
		envDur("DEBOUNCE_MIN", 8*time.Second),
		envDur("DEBOUNCE_MAX", 12*time.Second))
	sched := NewScheduler(db, q)        // dispara os timers (esperas/follow-ups)
	sched.Start(ctx)
	srv := NewServer(debounce)          // recebe os webhooks

	log.Printf("[cerebro] ouvindo em %s (api-escala=%s)", addr, apiURL)
	if err := http.ListenAndServe(addr, srv.Routes()); err != nil {
		log.Fatal(err)
	}
}
