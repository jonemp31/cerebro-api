package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// APIClient — cliente HTTP da api-escala. Só CHAMA os endpoints; não altera nada nela.
type APIClient struct {
	baseURL string
	http    *http.Client
}

func NewAPIClient(baseURL string) *APIClient {
	return &APIClient{
		baseURL: baseURL,
		// timeout folgado: o primeiro envio pode acordar a sessão (cold→hot leva alguns segundos).
		http: &http.Client{Timeout: 120 * time.Second},
	}
}

func (c *APIClient) SendText(ctx context.Context, sessionID, phone, text string) error {
	body, _ := json.Marshal(map[string]string{"phone": phone, "text": text})
	return c.post(ctx, fmt.Sprintf("/sessions/%s/send/text", sessionID), body)
}

func (c *APIClient) SendPix(ctx context.Context, sessionID, phone, keyType, name, key, instructions string) error {
	body, _ := json.Marshal(map[string]string{
		"phone": phone, "key_type": keyType, "name": name, "key": key, "instructions": instructions,
	})
	return c.post(ctx, fmt.Sprintf("/sessions/%s/send/pix", sessionID), body)
}

// SendAudioURL — envia áudio via URL. A api-escala baixa o arquivo e simula gravação.
func (c *APIClient) SendAudioURL(ctx context.Context, sessionID, phone, audioURL string) error {
	form := url.Values{
		"phone": {phone},
		"url":   {audioURL},
	}
	return c.postForm(ctx, fmt.Sprintf("/sessions/%s/send/audio", sessionID), form)
}

// SendImageURL — envia imagem via URL com caption e viewOnce opcionais.
func (c *APIClient) SendImageURL(ctx context.Context, sessionID, phone, imageURL, caption string, viewOnce bool) error {
	form := url.Values{
		"phone":   {phone},
		"url":     {imageURL},
		"caption": {caption},
	}
	if viewOnce {
		form.Set("view_once", "true")
	}
	return c.postForm(ctx, fmt.Sprintf("/sessions/%s/send/image", sessionID), form)
}

// AcceptVideo — arma auto-atender a próxima chamada de vídeo recebida.
// O vídeo da URL é transcodificado e usado como câmera+microfone fake.
// allowedNumbers filtra quem pode ativar o auto-accept (número do lead).
func (c *APIClient) AcceptVideo(ctx context.Context, sessionID, videoURL, allowedNumbers string) error {
	form := url.Values{
		"url":             {videoURL},
		"allowed_numbers": {allowedNumbers},
	}
	return c.postForm(ctx, fmt.Sprintf("/sessions/%s/call/accept-video", sessionID), form)
}

// SendTyping — mostra "digitando..." por durationMs. Retorna na hora (a api só
// aciona o indicador e agenda parar); quem espera a duração é o chamador.
func (c *APIClient) SendTyping(ctx context.Context, sessionID, chatID string, durationMs int) error {
	body, _ := json.Marshal(map[string]any{"chat_id": chatID, "duration_ms": durationMs})
	return c.post(ctx, fmt.Sprintf("/sessions/%s/chat/typing", sessionID), body)
}

func (c *APIClient) post(ctx context.Context, path string, body []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("api %s -> %d: %s", path, resp.StatusCode, string(b))
	}
	return nil
}

func (c *APIClient) postForm(ctx context.Context, path string, form url.Values) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("api %s -> %d: %s", path, resp.StatusCode, string(b))
	}
	return nil
}
