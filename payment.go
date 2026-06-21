package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// PaymentClient — cliente do gateway de pagamento (Nexus Pay).
type PaymentClient struct {
	createURL string // POST: cria cobrança Pix
	checkURL  string // GET:  consulta status
	http      *http.Client
}

// PixCharge — resposta da criação de cobrança.
type PixCharge struct {
	ID           string  `json:"id"`
	PixCopiaCola string  `json:"pix_copia_cola"`
	Amount       float64 `json:"amount"`
	Status       string  `json:"status"`
}

func NewPaymentClient(createURL, checkURL string) *PaymentClient {
	return &PaymentClient{
		createURL: createURL,
		checkURL:  checkURL,
		http:      &http.Client{Timeout: 30 * time.Second},
	}
}

// CreateCharge — cria uma cobrança Pix no gateway.
func (c *PaymentClient) CreateCharge(ctx context.Context, phone string, amount float64, description string) (*PixCharge, error) {
	body, _ := json.Marshal(map[string]any{
		"amount":      amount,
		"description": description,
		"external_id": phone,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.createURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("create charge request: %w", err)
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("create charge HTTP %d: %s", resp.StatusCode, string(raw))
	}

	// Resposta é um objeto: {"success":true,"transaction":{...}}
	var result struct {
		Success     bool      `json:"success"`
		Transaction PixCharge `json:"transaction"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("parse charge response: %w (body: %s)", err, string(raw))
	}
	if !result.Success {
		return nil, fmt.Errorf("charge creation failed (body: %s)", string(raw))
	}
	return &result.Transaction, nil
}

// CheckStatus — consulta o status de uma cobrança pelo ID.
func (c *PaymentClient) CheckStatus(ctx context.Context, chargeID string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.checkURL+"?id="+chargeID, nil)
	if err != nil {
		return "", err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("check status request: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("parse status response: %w", err)
	}
	return result.Status, nil
}
