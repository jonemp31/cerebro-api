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
	msgShowYou  = "te mostrar uma coisa"
	msgThatsMe  = "essa sou eu rs"
	msgLikedIt  = "gostou?"
	msgQuestion = "O que faz de bom por aí?"
	msgPixIntro = "Aqui meu pix"
)

// Áudios do funil.
const (
	audioGreeting = "https://s3.crushzap.com/painel/copy1/yas1.mp3"
)

// Imagens do funil.
const (
	imgProfile = "https://s3.crushzap.com/painel/copy1/yasfoto1.jpg"
)

// Dados do Pix (copia-e-cola enviado pelo botão Pix como "chave aleatória").
const (
	pixKeyType = "EVP" // EVP = chave aleatória
	pixName    = "🔒 PIX COPIA E COLA"
	pixKey     = "00020126360014BR.GOV.BCB.PIX011440066967000190520400005303986540530.005802BR5901N6001C62110507produto6304426F"
)

// Passos (steps) do funil.
const (
	stepNew        = ""              // lead novo / primeiro contato
	stepAwaitQ1    = "await_q1"      // mandou "gostou?", aguarda resposta
	stepAwaitQ1Fu1 = "await_q1_fu1"  // 1° follow-up enviado, aguarda resposta
	stepAwaitQ1Fu2 = "await_q1_fu2"  // 2° follow-up enviado, aguarda resposta (ou dorme)
	stepAwaitQ2    = "await_q2"      // mandou "O que faz de bom?", aguarda resposta
	stepPixSent    = "pix_sent"      // mandou o Pix, aguardando pagamento (próxima fase)
)

// ── Follow-ups do await_q1 ──────────────────────────────────────────────────

// Follow-up 1 — escolha aleatória (5 min sem resposta)
var followUp1 = []string{
	"ta aí amor? sumiu rs",
	"ei sumiu? rs",
	"oii cade vc?",
	"ta ocupado amor?",
}

func randomFollowUp1() string {
	return followUp1[rand.Intn(len(followUp1))]
}

// Follow-up 2 — sequência fixa (mais 5 min sem resposta)
const (
	msgFu2a = "poxa"
	msgFu2b = "vai me ignorar mesmo é 😕"
	msgFu2c = "??"
)

// Comeback — quando o lead volta depois do follow-up pesado (fu2)
const (
	msgComebackA = "até q enfim né rsrs"
	msgComebackB = "achei q tinha me abandonado aqui rs"
	msgComebackC = "faz isso mais n pfv tá"
)
