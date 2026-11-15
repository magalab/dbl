package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
)

func modelsEndpoint(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "https" && u.Scheme != "http") {
		return "", errors.New("invalid AI endpoint")
	}
	if u.Scheme == "http" && u.Hostname() != "localhost" && u.Hostname() != "127.0.0.1" && u.Hostname() != "::1" {
		return "", errors.New("AI endpoint must use HTTPS except for localhost providers")
	}
	path := strings.TrimRight(u.Path, "/")
	path = strings.TrimSuffix(path, "/chat/completions")
	if !strings.HasSuffix(path, "/models") {
		path += "/models"
	}
	u.Path, u.RawPath = path, ""
	return u.String(), nil
}

// Models discovers IDs without sending prompts or database context.
func (p OpenAICompatible) Models(ctx context.Context) ([]string, error) {
	endpoint, err := modelsEndpoint(p.Endpoint)
	if err != nil {
		return nil, err
	}
	if len(p.APIKey) == 0 {
		return nil, errors.New("API key is required")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+string(p.APIKey))
	req.Header.Set("Accept", "application/json")
	client := p.Client
	if client == nil {
		client = http.DefaultClient
	}
	copyClient := *client
	copyClient.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := copyClient.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, errors.New("model list request failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("model list returned HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil || len(body) > maxResponseBytes {
		return nil, errors.New("model list read failed or exceeds size limit")
	}
	var payload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, errors.New("invalid model list response")
	}
	seen := map[string]bool{}
	models := []string{}
	for _, model := range payload.Data {
		id := strings.TrimSpace(model.ID)
		if id != "" && !seen[id] {
			seen[id] = true
			models = append(models, id)
		}
	}
	sort.Strings(models)
	if len(models) == 0 {
		return nil, errors.New("provider returned no model IDs")
	}
	return models, nil
}
