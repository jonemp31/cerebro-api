package main

import (
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

// Humanização (configurável por env). Defaults pedidos:
//   delay de leitura: 30s a 90s
//   digitação: 3s + 60ms/caractere, com teto de 15s
var (
	cfgMinDelay      = envDur("MIN_REPLY_DELAY", 30*time.Second)
	cfgMaxDelay      = envDur("MAX_REPLY_DELAY", 90*time.Second)
	cfgTypingBase    = envDur("TYPING_BASE", 3*time.Second)
	cfgTypingPerChar = envDur("TYPING_PER_CHAR", 60*time.Millisecond)
	cfgTypingCap     = envDur("TYPING_CAP", 15*time.Second)
)
