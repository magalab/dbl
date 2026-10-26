package assistant

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestOpenAICompatibleStreamsOnlyPreparedMessages(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("request method = %s, want POST", r.Method)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("authorization = %q", got)
		}
		if got := r.Header.Get("Accept"); got != "text/event-stream" {
			t.Errorf("Accept = %q", got)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request body: %v", err)
		}
		const wantBody = `{"model":"test-model","stream":true,"messages":[{"role":"user","content":"only this prompt"}]}`
		if string(body) != wantBody {
			t.Errorf("request body = %s, want exactly %s", body, wantBody)
		}
		if strings.Contains(string(body), "test-key") {
			t.Error("API key was included in the JSON request body")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"hello \"}}]}\n\n"))
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"world\"}}]}\n\ndata: [DONE]\n\n"))
	}))
	defer server.Close()

	var chunks []string
	provider := OpenAICompatible{Endpoint: server.URL, Model: "test-model", APIKey: []byte("test-key")}
	err := provider.Stream(context.Background(), []Message{{Role: "user", Content: "only this prompt"}}, func(chunk string) error {
		chunks = append(chunks, chunk)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(chunks, "") != "hello world" || len(chunks) != 2 {
		t.Fatalf("streamed chunks = %#v", chunks)
	}
}

func TestOpenAICompatibleDoesNotFollowRedirects(t *testing.T) {
	var targetRequests atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetRequests.Add(1)
	}))
	defer target.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	defer origin.Close()

	provider := OpenAICompatible{Endpoint: origin.URL, Model: "model", APIKey: []byte("secret")}
	err := provider.Stream(context.Background(), []Message{{Role: "user", Content: "prompt"}}, nil)
	if err == nil || !strings.Contains(err.Error(), "HTTP 302") {
		t.Fatalf("redirect response error = %v, want HTTP 302", err)
	}
	if got := targetRequests.Load(); got != 0 {
		t.Fatalf("redirect target received %d request(s)", got)
	}
}

func TestOpenAICompatibleRejectsUnsafeConfiguration(t *testing.T) {
	for _, test := range []struct {
		name     string
		endpoint string
		model    string
		key      []byte
		messages []Message
	}{
		{name: "missing endpoint", model: "model", key: []byte("secret"), messages: []Message{{Role: "user", Content: "prompt"}}},
		{name: "empty message list", endpoint: "https://provider.example/v1/chat/completions", model: "model", key: []byte("secret")},
		{name: "remote cleartext", endpoint: "http://provider.example/chat", model: "model", key: []byte("secret"), messages: []Message{{Role: "user", Content: "prompt"}}},
		{name: "endpoint credentials", endpoint: "https://user:pass@provider.example/chat", model: "model", key: []byte("secret"), messages: []Message{{Role: "user", Content: "prompt"}}},
		{name: "endpoint query", endpoint: "https://provider.example/chat?token=secret", model: "model", key: []byte("secret"), messages: []Message{{Role: "user", Content: "prompt"}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := OpenAICompatible{Endpoint: test.endpoint, Model: test.model, APIKey: test.key}
			if err := provider.Stream(context.Background(), test.messages, nil); err == nil {
				t.Fatal("unsafe or incomplete AI configuration was accepted")
			}
		})
	}
}

func TestOpenAICompatibleCancellationStopsStream(t *testing.T) {
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(": connected\n\n"))
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		close(started)
		<-r.Context().Done()
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	provider := OpenAICompatible{Endpoint: server.URL, Model: "model", APIKey: []byte("secret")}
	go func() {
		done <- provider.Stream(ctx, []Message{{Role: "user", Content: "prompt"}}, nil)
	}()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("provider did not start the HTTP stream")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled request error = %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("provider did not stop after cancellation")
	}
}
