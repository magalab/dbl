package assistant

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (roundTrip roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return roundTrip(request)
}

func testHTTPResponse(request *http.Request, status int, headers http.Header, body string) *http.Response {
	if headers == nil {
		headers = make(http.Header)
	}
	return &http.Response{
		StatusCode: status,
		Header:     headers,
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    request,
	}
}

func TestOpenAICompatibleStreamsOnlyPreparedMessages(t *testing.T) {
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.Method != http.MethodPost {
			t.Errorf("request method = %s, want POST", request.Method)
		}
		if got := request.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("authorization = %q", got)
		}
		if got := request.Header.Get("Accept"); got != "text/event-stream" {
			t.Errorf("Accept = %q", got)
		}
		body, err := io.ReadAll(request.Body)
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
		return testHTTPResponse(request, http.StatusOK, http.Header{"Content-Type": []string{"text/event-stream"}},
			"data: {\"choices\":[{\"delta\":{\"content\":\"hello \"}}]}\n\n"+
				"data: {\"choices\":[{\"delta\":{\"content\":\"world\"}}]}\n\ndata: [DONE]\n\n"), nil
	})

	var chunks []string
	provider := OpenAICompatible{Endpoint: "https://provider.example/v1/chat/completions", Model: "test-model", APIKey: []byte("test-key"), Client: &http.Client{Transport: transport}}
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
	var requests atomic.Int32
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		requests.Add(1)
		if request.URL.Host == "target.example" {
			targetRequests.Add(1)
		}
		location, _ := url.Parse("https://target.example/redirected")
		return testHTTPResponse(request, http.StatusFound, http.Header{"Location": []string{location.String()}}, ""), nil
	})

	provider := OpenAICompatible{Endpoint: "https://origin.example/chat", Model: "model", APIKey: []byte("secret"), Client: &http.Client{Transport: transport}}
	err := provider.Stream(context.Background(), []Message{{Role: "user", Content: "prompt"}}, nil)
	if err == nil || !strings.Contains(err.Error(), "HTTP 302") {
		t.Fatalf("redirect response error = %v, want HTTP 302", err)
	}
	if got := targetRequests.Load(); got != 0 {
		t.Fatalf("redirect target received %d request(s)", got)
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("transport received %d request(s), want only the original request", got)
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
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		close(started)
		<-request.Context().Done()
		return nil, request.Context().Err()
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	provider := OpenAICompatible{Endpoint: "http://localhost/v1/chat/completions", Model: "model", APIKey: []byte("secret"), Client: &http.Client{Transport: transport}}
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

func TestChatAcceptsBaseURLAndFullEndpoint(t *testing.T) {
	for _, endpoint := range []string{"https://provider.example/v1", "https://provider.example/v1/", "https://provider.example/v1/chat/completions", "https://provider.example"} {
		transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
			if request.URL.Path != "/v1/chat/completions" {
				t.Errorf("chat path = %s", request.URL.Path)
			}
			return testHTTPResponse(request, http.StatusOK, nil, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\ndata: [DONE]\n\n"), nil
		})
		p := OpenAICompatible{Endpoint: endpoint, Model: "model", APIKey: []byte("key"), Client: &http.Client{Transport: transport}}
		if err := p.Stream(context.Background(), []Message{{Role: "user", Content: "hello"}}, nil); err != nil {
			t.Fatal(err)
		}
	}
}
