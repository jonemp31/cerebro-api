package main

import (
	"context"
	"log"
	"net/http"
	"os"
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
	eng := NewEngine(db, api)           // a máquina de estados (o cérebro)
	q := NewQueue(eng, 512)             // fila 1-a-1 (channel + 1 worker)
	q.Start()
	sched := NewScheduler(db, q)        // dispara os timers (esperas/follow-ups)
	sched.Start(ctx)
	srv := NewServer(q)                 // recebe os webhooks

	log.Printf("[cerebro] ouvindo em %s (api-escala=%s)", addr, apiURL)
	if err := http.ListenAndServe(addr, srv.Routes()); err != nil {
		log.Fatal(err)
	}
}
