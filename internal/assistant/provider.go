// Package assistant provides replaceable cloud AI providers.
package assistant

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

const maxResponseBytes = 1 << 20

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}
type Provider interface {
	Stream(context.Context, []Message, func(string) error) error
}

// OpenAICompatible uses the common chat-completions SSE protocol without a
// vendor SDK, so providers can be changed by configuration.
type OpenAICompatible struct {
	Endpoint, Model string
	APIKey          []byte
	Client          *http.Client
}

func (provider OpenAICompatible) Stream(ctx context.Context, messages []Message, emit func(string) error) error {
	if strings.TrimSpace(provider.Endpoint) == "" || strings.TrimSpace(provider.Model) == "" || len(provider.APIKey) == 0 {
		return errors.New("AI endpoint, model, and API key are required")
	}
	if len(messages) == 0 {
		return errors.New("AI request has no messages")
	}
	endpoint, err := url.Parse(provider.Endpoint)
	if err != nil || endpoint.Host == "" || (endpoint.Scheme != "https" && endpoint.Scheme != "http") || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" {
		return errors.New("AI endpoint must be an http(s) URL without credentials, query, or fragment")
	}
	if endpoint.Scheme != "https" && endpoint.Hostname() != "localhost" && endpoint.Hostname() != "127.0.0.1" && endpoint.Hostname() != "::1" {
		return errors.New("AI endpoint must use HTTPS except for localhost providers")
	}
	payload := struct {
		Model    string    `json:"model"`
		Stream   bool      `json:"stream"`
		Messages []Message `json:"messages"`
	}{Model: provider.Model, Stream: true, Messages: messages}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, provider.Endpoint, bytes.NewReader(body))
	if err != nil {
		return errors.New("invalid AI endpoint")
	}
	req.Header.Set("Authorization", "Bearer "+string(provider.APIKey))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	client := provider.Client
	if client == nil {
		client = http.DefaultClient
	}
	copyClient := *client
	copyClient.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
	client = &copyClient
	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("AI request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("AI provider returned HTTP %d", resp.StatusCode)
	}
	scanner := bufio.NewScanner(io.LimitReader(resp.Body, maxResponseBytes+1))
	scanner.Buffer(make([]byte, 4096), 1<<20)
	var total int
	sawDone := false
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return err
		}
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			sawDone = true
			break
		}
		var event struct {
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
			} `json:"choices"`
		}
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			return errors.New("AI provider returned invalid streaming data")
		}
		if len(event.Choices) == 0 {
			continue
		}
		chunk := event.Choices[0].Delta.Content
		total += len(chunk)
		if total > maxResponseBytes {
			return errors.New("AI response exceeds 1 MiB limit")
		}
		if chunk != "" && emit != nil {
			if err := emit(chunk); err != nil {
				return err
			}
		}
	}
	if err := scanner.Err(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errors.New("AI response stream failed or exceeded its line limit")
	}
	if !sawDone {
		return errors.New("AI response stream ended before completion")
	}
	if total == 0 {
		return errors.New("AI provider returned no response content")
	}
	return nil
}
