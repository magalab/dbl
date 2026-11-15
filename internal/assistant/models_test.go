package assistant

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestModelsEndpointDiscoveryAndAuthentication(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/v1/models" || r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("unexpected model request: %s %s", r.Method, r.URL.Path)
		}
		if r.ContentLength > 0 {
			t.Error("model request contains a body")
		}
		w.Write([]byte(`{"data":[{"id":"z-model"},{"id":"a-model"},{"id":"z-model"},{"id":""}]}`))
	}))
	defer server.Close()
	for _, path := range []string{"/v1", "/v1/", "/v1/chat/completions"} {
		models, err := (OpenAICompatible{Endpoint: server.URL + path, APIKey: []byte("test-key")}).Models(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if strings.Join(models, ",") != "a-model,z-model" {
			t.Fatalf("models = %v", models)
		}
	}
}

func TestModelDiscoveryRejectsRedirectsAndInvalidResponses(t *testing.T) {
	for _, body := range []string{`{}`, `not json`, strings.Repeat("x", maxResponseBytes+1)} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(body)) }))
		_, err := (OpenAICompatible{Endpoint: server.URL, APIKey: []byte("test-key")}).Models(context.Background())
		server.Close()
		if err == nil {
			t.Fatal("accepted invalid model response")
		}
	}
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("followed redirect with credentials") }))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, http.StatusFound) }))
	defer redirect.Close()
	if _, err := (OpenAICompatible{Endpoint: redirect.URL, APIKey: []byte("test-key")}).Models(context.Background()); err == nil {
		t.Fatal("accepted redirect")
	}
	for _, raw := range []string{"http://remote.example/v1", "https://user:pass@example.com/v1", "https://example.com/v1?key=secret"} {
		if _, err := modelsEndpoint(raw); err == nil {
			t.Fatalf("accepted unsafe endpoint %s", raw)
		}
	}
}
