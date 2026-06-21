package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// PaymentClient — cliente de consulta de pagamento.
type PaymentClient struct {
	checkURL string // POST: consulta status do PIX
	http     *http.Client
}

func NewPaymentClient(checkURL string) *PaymentClient {
	return &PaymentClient{
		checkURL: checkURL,
		http:     &http.Client{Timeout: 30 * time.Second},
	}
}

// CheckStatus — consulta o status de um PIX via webhook.
// Retorna (status normalizado, nome do pagador, erro).
func (c *PaymentClient) CheckStatus(ctx context.Context, phone string, amount float64, createdAt time.Time) (string, string, error) {
	// Converte pra horário de Brasília
	loc, _ := time.LoadLocation("America/Sao_Paulo")
	br := createdAt.In(loc)

	body, _ := json.Marshal(map[string]any{
		"data":  br.Format("02/01/2006"), // DD/MM/YYYY
		"hora":  br.Format("15:04"),      // HH:MM
		"phone": phone,
		"valor": amount,
	})

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.checkURL, bytes.NewReader(body))
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("check status request: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		Status string `json:"status"`
		Nome   string `json:"nome"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", "", fmt.Errorf("parse status response: %w", err)
	}

	// Normaliza a resposta
	switch result.Status {
	case "aprovado":
		return "paid", result.Nome, nil
	case "expirado":
		return "expired", result.Nome, nil
	default:
		return "pending", result.Nome, nil
	}
}
