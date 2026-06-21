package main

import "sync"

// KeyedMutex — um mutex POR CHAVE (ex.: por lead). Permite processar leads
// diferentes em paralelo, mas serializa tudo de um MESMO lead (sem corrida,
// sem duplicação). Conta referências para limpar a entrada quando ninguém usa.
type KeyedMutex struct {
	mu    sync.Mutex
	locks map[string]*keyedEntry
}

type keyedEntry struct {
	mu  sync.Mutex
	ref int
}

func NewKeyedMutex() *KeyedMutex {
	return &KeyedMutex{locks: make(map[string]*keyedEntry)}
}

func (k *KeyedMutex) Lock(key string) {
	k.mu.Lock()
	e, ok := k.locks[key]
	if !ok {
		e = &keyedEntry{}
		k.locks[key] = e
	}
	e.ref++
	k.mu.Unlock()
	e.mu.Lock()
}

func (k *KeyedMutex) Unlock(key string) {
	k.mu.Lock()
	e := k.locks[key]
	if e != nil {
		e.ref--
		if e.ref == 0 {
			delete(k.locks, key)
		}
	}
	k.mu.Unlock()
	if e != nil {
		e.mu.Unlock()
	}
}
