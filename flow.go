package main

// ─────────────────────────────────────────────────────────────────────────────
// Copy do funil — Fase 1 (fixa no código). Edite os textos aqui.
// No futuro isto vira o "construtor de fluxos" (data-driven no banco).
// ─────────────────────────────────────────────────────────────────────────────

const (
	msgGreeting = "Oi, tudo bem?"
	msgQuestion = "O que faz de bom por aí?"
	msgPixIntro = "Aqui meu pix"
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
