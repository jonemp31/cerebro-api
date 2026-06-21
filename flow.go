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
	msgShowYou   = "te mostrar uma coisa"
	msgThatsMe   = "essa sou eu rs"
	msgLikedIt   = "gostou?"
	msgAlone     = "vc tá sozinho aí agora? 🙈"
	msgAloneFu   = "?"
	msgGift      = "vou te dar um presente agora, tá? ❤️"
	msgLive      = "quer me ver ao vivo?"
	msgEnjoy     = "aproveita"
	msgNotAnyone = "pq eu não faço isso pra qualquer um não, viu?"
	msgLikedYou  = "só pq gostei de vc"
	msgCallMe    = "me liga aqui por chamada de vídeo rapidinho"
	msgOnWA      = "no whatsapp mesmo"
	msgNotFake   = "só pra você ver q eu não sou fake rs"
	msgShowU     = "qro te mostrar uma coisa rs"
	msgCallNow   = "liga agora vai"
	msgWaiting   = "to aqui te esperando ❤️🔥"
	msgDoneCall  = "pronto gatinho"
	msgLikedCall = "gostou rs?"
	msgLikedU2   = "gostei de vc, sabia? rs ❤️"
	msgThinking  = "sabe oq eu tava pensando?"
	msgWhatAbout = "oq vc acha da gente"
	msgContinue  = "continuar"
	msgThisCall  = "essa chamada de vídeo?"
	msgLike      = "tipo"
	msgBothHere  = "vc ta aí, eu to aqui"
	msgJustUs    = "só nos dois sozinhos rs"
	msgWanna     = "topa?"
	msgHelpToo   = "se vc aceitar e puder me ajudar tbm rs"
	msgAskPix    = "me ajuda com 20 reais no pix amor? 🥹🙏"
	msgCanSendPix  = "posso te mandar meu pix?"
	msgSendPix     = "manda o pix e eu já te ligo agora"
	msgCopyKey     = "só copiar a chave amor 👇"
	msgSendReceipt = "e me mandar o comprovante aqui"
	msgDealAmor    = "combinado amor?"
	msgWaitHeart   = "to te esperando aqui 🙈❤️"
	msgPixIntro    = "Aqui meu pix"
)

// Áudios do funil.
const (
	audioGreeting = "https://s3.crushzap.com/painel/copy1/yas1.mp3"
	audioYas2     = "https://s3.crushzap.com/painel/copy1/yas2.mp3"
	audioYas3     = "https://s3.crushzap.com/painel/copy1/yas3.mp3"
	audioYas4     = "https://s3.crushzap.com/painel/copy1/yas4.mp3"
	audioYas5     = "https://s3.crushzap.com/painel/copy1/yas5.mp3"
	audioYas6     = "https://s3.crushzap.com/painel/copy1/yas6.mp3"
	audioYas7     = "https://s3.crushzap.com/painel/copy1/yas7.mp3"
	audioYas8     = "https://s3.crushzap.com/painel/copy1/yas8.mp3"
)

// Imagens do funil.
const (
	imgProfile  = "https://s3.crushzap.com/painel/copy1/yasfoto1.jpg"
	imgViewOnce = "https://s3.crushzap.com/painel/copy1/yasfoto4.jpg"
	imgFotoB1   = "https://s3.crushzap.com/painel/copy1/yasfotob1.jpg"
	imgFotoB2   = "https://s3.crushzap.com/painel/copy1/yasfotob2.jpg"
)

// Vídeos do funil (chamada de vídeo).
const (
	videoCall1    = "https://s3.crushzap.com/painel/copy1/videoligacao1.mp4"
	videoEntrega1 = "https://s3.crushzap.com/painel/copy1/entrega1.mp4"
	videoEntrega2 = "https://s3.crushzap.com/painel/copy1/entrega2.mp4"
)

// Imagens do funil (upsell).
const (
	imgUpsellApp = "https://s3.crushzap.com/painel/copy1/yasapp.png"
)

// Dados do Pix (copia-e-cola enviado pelo botão Pix como "chave aleatória").
const (
	pixKeyType = "EVP" // EVP = chave aleatória
	pixName    = "🔒 PIX COPIA E COLA"
	pixKey     = "00020126360014BR.GOV.BCB.PIX011440066967000190520400005303986540530.005802BR5901N6001C62100506Yasmin63048B98"
)

// Passos (steps) do funil.
const (
	stepNew         = ""               // lead novo / primeiro contato
	stepAwaitQ1     = "await_q1"       // mandou "gostou?", aguarda resposta
	stepAwaitQ1Fu1  = "await_q1_fu1"   // 1° follow-up enviado, aguarda resposta
	stepAwaitQ1Fu2  = "await_q1_fu2"   // 2° follow-up enviado (ou dorme)
	stepAwaitQ2     = "await_q2"       // mandou "vc tá sozinho?", aguarda resposta
	stepAwaitQ2Fu1  = "await_q2_fu1"   // follow-up "?" enviado (dorme)
	stepAwaitQ3     = "await_q3"       // mandou "vou te dar um presente", aguarda resposta
	stepAwaitQ4     = "await_q4"       // mandou "só pq gostei de vc", aguarda resposta (timer 3min)
	stepCallArmed   = "call_armed"     // vídeo-chamada armada — tentativa 1
	stepCallArmed2  = "call_armed_2"   // tentativa 2
	stepCallArmed3  = "call_armed_3"   // tentativa 3
	stepCallArmed4  = "call_armed_4"   // tentativa 4 (última)
	stepCallGiveUp  = "call_give_up"   // desistiu — lead não ligou em nenhuma tentativa
	stepAwaitQ5     = "await_q5"       // mandou "topa?", aguarda resposta
	stepAwaitQ6     = "await_q6"       // mandou "posso te mandar meu pix?", aguarda resposta
	stepPixSent     = "pix_sent"       // 1° PIX enviado, polling
	stepPixSent2    = "pix_sent_2"     // 2° PIX enviado (retry após expirar), polling
	stepPixSent2Fu  = "pix_sent_2_fu"  // follow-up 10min do 2° PIX enviado, ainda polling
	stepPixExpired  = "pix_expired"    // 2° PIX também expirou — lead perdido
	// ── Pós-pagamento (entrega) ──
	stepDeliveryCallArmed  = "delivery_call"    // chamada de entrega armada — tentativa 1
	stepDeliveryCallArmed2 = "delivery_call_2"  // tentativa 2
	stepDeliveryGiveUp     = "delivery_give_up" // não ligou pra entrega
	stepUpsellPixSent      = "upsell_pix_sent"  // upsell PIX enviado, polling
	stepUpsellPixSentFu    = "upsell_pix_fu"    // follow-up 10min do upsell PIX
	stepUpsellDeliveryArmed = "upsell_delivery"  // chamada entrega2 armada
	stepDone               = "done"             // funil completo
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

// ── Follow-ups de chamada (lead não ligou) ──────────────────────────────────

// Call follow-up 1 — 1ª vez que expirou
const (
	msgCf1a = "poxa amor"
	msgCf1b = "não vai me ligar não?"
	msgCf1c = "to aqui te esperando, me liga vai"
	msgCf1d = "rapidinho"
	msgCf1e = "só pra vc me ver vai"
	msgCf1f = "n tenho mto tempo"
)

// Call follow-up 2 — 2ª vez que expirou
const (
	msgCf2a = "amor"
	msgCf2b = "ta aí?"
	msgCf2c = "prometo que"
	msgCf2d = "você não vai se arrepender"
	msgCf2e = "me liga de chamada de vídeo"
	msgCf2f = "aqui pra mim agora"
	msgCf2g = "vai?"
)

// Call follow-up 3 — 3ª vez que expirou (último)
const (
	msgCf3a = "bom"
	msgCf3b = "fiquei te esperando aqui"
	msgCf3c = "e vc n quis, né?"
	msgCf3d = "perdeu rs"
	msgCf3e = "agora só mais tarde"
	msgCf3f = "nos falamos dps"
	msgCf3g = "bjo"
)

// ── PIX expirado → retry com valor menor ────────────────────────────────────

const (
	msgPixRetryA   = "amor, tá por aí?"
	msgPixRetryB   = "precisava falar com vc rapidinho"
	msgPixRetryC   = "tem 1 minutinho? ❤️"
	msgPixRetryD   = "rapidinhoo"
	msgPixRetryE   = "ouve qnd vc puder por favor 😌"
	msgPixRetryF   = "deu certo?"
)

// ── PIX 2 — follow-up 10 min sem pagar ──────────────────────────────────────

const (
	msgPixFuA = "poxa amor"
	msgPixFuB = "vai me deixar aqui te esperando mesmo?"
	msgPixFuC = "preciso ir tomar banho..."
	msgPixFuD = "vem ser feliz, vem?"
	msgPixFuE = "vem me ver"
)

// ── Compra aprovada (paid) ──────────────────────────────────────────────────

const (
	msgPd01 = "notificou aqui, perai amor"
	msgPd02 = "vou confirmar aqui"
	msgPd03 = "muitooo obrigada viu amor? ❤️"
	msgPd04 = "vc é incrível 🎉"
	msgPd05 = "vai me ajudar mtooo aqui, obg msm 🙏🙏"
	msgPd06 = "de coração mesmo viu? 😍"
	msgPd07 = "pronto pra receber o seu prêmio? 🔥"
	msgPd08 = "seus presentinhos rs"
	msgPd09 = "afinal vc merece, né? vou liberar tudo aqui agora pra você"
	msgPd10 = "bem rapidinho"
	msgPd11 = "e vc me liga dps, tá bom?"
	msgPd12 = "1 minutinhoo"
	msgPd13 = "te mandar tudo aqui 🙈"
	msgPd14 = "prontinhooo"
	msgPd15 = "só acessar esse link: https://bit.ly/VipYasAqui"
	msgPd16 = "vai pedir uma senha (1011)"
	msgPd17 = "sua senha vip é: 1011"
	msgPd18 = "tá? aí tem tudo q te prometi"
	msgPd19 = "acessa ai e me avisa se deu certo?"
	msgPd20 = "pfvzinhoo 🙏🥹"
	msgPd21 = "foi?"
	msgPd22 = "espero que vc goste viu rs ❤️"
	msgPd23 = "sobre a chamada de vídeo q vc ganhou de presente"
	msgPd24 = "como vc quer fazer?"
	msgPd25 = "quer q eu te ligue agora"
	msgPd26 = "ou em outro horário q for melhor pra vc?"
	msgPd27 = "só me avisa aqui pra"
	msgPd28 = "eu organizar direitinho contigo"
	msgPd29 = "se vc puder anjo"
	msgPd30 = "me liga agora vai"
	msgPd31 = "qro te mostrar uma coisa"
	msgPd32 = "veeeem"
	msgPd33 = "me liga de vídeo aqui"
	msgPd34 = "rapido anjo"
	msgPd35 = "tem q ser agora"
)

// ── Delivery call follow-up (não ligou) ─────────────────────────────────────

const (
	msgDcf1a = "amor to aqui agora"
	msgDcf1b = "me liga vai ser rapidinho"
	msgDcf1c = "dps n vou poder, aí só amanha"
	msgDcf1d = "me liga aqui"
	msgDcf1e = "vou te esperar por 5 min só"
	msgDcf1f = "vem?"
)

const (
	msgDcf2a = "vc n vem msm então né?"
	msgDcf2b = "fiquei te esperando atoa"
	msgDcf2c = "dps qnd vc puder a gente se fala então"
	msgDcf2d = "me chama qnd puder cvs"
	msgDcf2e = "bjo"
)

// ── Upsell (pós-chamada entrega) ────────────────────────────────────────────

const (
	msgUp01 = "gostou amorzinho? ❤️"
	msgUp02 = "quer continuar?"
	msgUp03 = "vamos?"
	msgUp04 = "me ajuda só com mais 15 reais pfv"
	msgUp05 = "que eu continuo até qnd vc quiser"
	msgUp06 = "prometo"
	msgUp07 = "qro comprar isso"
	msgUp08 = "ta na promoção só agora rs"
	msgUp09 = "topa amor?"
	msgUp10 = "me ajuda pfv e eu te recompenso dps"
	msgUp11 = "te mandar meu pix"
	msgUp12 = "só fazer e aí a gente continua a chamada"
)

// ── Upsell PIX pago ─────────────────────────────────────────────────────────

const (
	msgUpPd1  = "caiu aqui o pix amor"
	msgUpPd2  = "mto obrigada viu?"
	msgUpPd3  = "já pode me ligar"
	msgUpPd4  = "me liga pra gente continuar"
	msgUpPd5  = "vamos? ❤️"
	msgUpFu   = "sumiu? 😔"
)
