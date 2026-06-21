package main

import "math/rand"

// ─────────────────────────────────────────────────────────────────────────────
// Copy do funil — Fase 1 (fixa no código). Edite os textos aqui.
// No futuro isto vira o "construtor de fluxos" (data-driven no banco).
// ─────────────────────────────────────────────────────────────────────────────

// Saudações — escolha aleatória a cada lead novo.
var greetings = []string{
	"Oi, tudo bem?",
	"Oi amor, tudo bem?",
	"Oi vida, tudo bem?",
	"Oi anjo, tudo bem?",
	"Oi gatinho, tudo bem?",
	"Oie, tudo bem?",
	"Oii, tudo bem?",
	"Oiii, tudo bem?",
	"ooi tudo bem?",
	"oi tudo bem?",
	"oiee tudo bem?",
	"oi bb, tudo bem?",
	"Oi bb, tudo bem?",
	"oi vida, tudo bem?",
	"oi anjo, tudo bem?",
	"oi gatinho, tudo bem?",
	"oi lindo, tudo bem?",
	"Oi nego, tudo bem?",
	"Oie amor, tudo bem?",
	"Oi meu bem, tudo bem?",
	"Oii vida, tudo bem?",
	"Oi neném, tudo bem?",
	"Oie anjo, tudo bem?",
	"Oi mozão, tudo bem?",
	"Oii gato, tudo bem?",
	"Oiee bb, tudo bem?",
}

func randomGreeting() string {
	return greetings[rand.Intn(len(greetings))]
}

const (
	msgQuestion = "O que faz de bom por aí?"
	msgPixIntro = "Aqui meu pix"
)

// Áudios do funil.
const (
	audioGreeting = "https://s3.crushzap.com/painel/copy1/yas1.mp3"
)

// Dados do Pix (copia-e-cola enviado pelo botão Pix como "chave aleatória").
const (
	pixKeyType = "EVP" // EVP = chave aleatória
	pixName    = "🔒 PIX COPIA E COLA"
	pixKey     = "00020126360014BR.GOV.BCB.PIX011440066967000190520400005303986540530.005802BR5901N6001C62110507produto6304426F"
)

// Passos (steps) do funil.
const (
	stepNew     = ""            // lead novo / primeiro contato
	stepAwaitQ1 = "await_q1"    // mandou "Oi, tudo bem?", aguarda resposta
	stepAwaitQ2 = "await_q2"    // mandou "O que faz de bom?", aguarda resposta
	stepPixSent = "pix_sent"    // mandou o Pix, aguardando pagamento (próxima fase)
)
