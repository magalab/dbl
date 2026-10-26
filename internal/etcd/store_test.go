package etcd

import (
	"errors"
	"reflect"
	"testing"

	"github.com/magalab/dbl/internal/connection"
	mvccpb "go.etcd.io/etcd/api/v3/mvccpb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestNormalizeEndpoints(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		port      int
		tlsMode   string
		want      []string
		wantTLS   bool
		wantError bool
	}{
		{name: "bare host defaults to tls", input: "etcd.internal", tlsMode: "verify-full", want: []string{"https://etcd.internal:2379"}, wantTLS: true},
		{name: "bare ipv4 uses provided port", input: "127.0.0.1", port: 12379, tlsMode: "verify-full", want: []string{"https://127.0.0.1:12379"}, wantTLS: true},
		{name: "ipv6 host gets brackets", input: "2001:db8::1", tlsMode: "disable", want: []string{"http://[2001:db8::1]:2379"}},
		{name: "explicit url ports", input: "https://one:12379,https://two:22379", tlsMode: "", want: []string{"https://one:12379", "https://two:22379"}, wantTLS: true},
		{name: "bare host tls disabled", input: "localhost:2379", tlsMode: "disable", want: []string{"http://localhost:2379"}},
		{name: "scheme mismatch", input: "http://localhost:2379", tlsMode: "verify-full", wantError: true},
		{name: "invalid scheme", input: "grpc://localhost:2379", tlsMode: "disable", wantError: true},
		{name: "url credentials rejected", input: "https://user:pass@localhost:2379", tlsMode: "verify-full", wantError: true},
		{name: "empty endpoint rejected", input: "localhost:2379,", tlsMode: "disable", wantError: true},
		{name: "invalid port rejected", input: "localhost", port: 65536, tlsMode: "disable", wantError: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, gotTLS, err := normalizeEndpoints(test.input, test.port, test.tlsMode)
			if test.wantError {
				if err == nil {
					t.Fatalf("normalizeEndpoints(%q) unexpectedly succeeded: %#v", test.input, got)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, test.want) || gotTLS != test.wantTLS {
				t.Fatalf("normalizeEndpoints(%q) = %#v, TLS %v; want %#v, TLS %v", test.input, got, gotTLS, test.want, test.wantTLS)
			}
		})
	}
}

func TestEtcdBoundsAndClosedStore(t *testing.T) {
	for _, test := range []struct{ input, want int }{{-1, 50}, {0, 50}, {1, 1}, {100, 100}, {500, 100}} {
		if got := bounded(test.input); got != test.want {
			t.Errorf("bounded(%d) = %d, want %d", test.input, got, test.want)
		}
	}
	for _, test := range []struct {
		key     string
		wantErr bool
	}{
		{key: "", wantErr: true},
		{key: "valid"},
		{key: string(make([]byte, 4097)), wantErr: true},
	} {
		err := validateKey(test.key)
		if (err != nil) != test.wantErr {
			t.Errorf("validateKey length %d error = %v, want error %v", len(test.key), err, test.wantErr)
		}
	}

	var store *Store
	if err := store.HealthCheck(t.Context()); err == nil {
		t.Fatal("HealthCheck on nil store succeeded")
	}
	if _, err := store.GetKey(t.Context(), "key"); err == nil {
		t.Fatal("GetKey on nil store succeeded")
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close nil store: %v", err)
	}
}

func TestEtcdClassifyErrors(t *testing.T) {
	tests := []struct {
		err  error
		want connection.ErrorKind
	}{
		{err: status.Error(codes.Unauthenticated, "auth"), want: connection.ErrorPermission},
		{err: status.Error(codes.PermissionDenied, "denied"), want: connection.ErrorPermission},
		{err: status.Error(codes.DeadlineExceeded, "late"), want: connection.ErrorTimeout},
		{err: status.Error(codes.Unavailable, "down"), want: connection.ErrorBusy},
		{err: status.Error(codes.InvalidArgument, "bad request"), want: connection.ErrorQuery},
		{err: errors.New("unexpected"), want: connection.ErrorUnknown},
	}
	for _, test := range tests {
		store := &Store{}
		if got := store.ClassifyError(test.err); got != test.want {
			t.Errorf("ClassifyError(%v) = %v, want %v", test.err, got, test.want)
		}
	}
}

func TestEtcdWatchEventReportsTruncatedValue(t *testing.T) {
	for _, test := range []struct {
		name      string
		valueSize int
		truncated bool
	}{
		{name: "at limit", valueSize: maxValueBytes},
		{name: "over limit", valueSize: maxValueBytes + 1, truncated: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			value := make([]byte, test.valueSize)
			got := watchEventFromKV("PUT", &mvccpb.KeyValue{
				Key: []byte("sample/key"), Value: value, ModRevision: 7, Version: 3, Lease: 42,
			})
			if got.ValueTruncated != test.truncated {
				t.Fatalf("ValueTruncated = %v, want %v", got.ValueTruncated, test.truncated)
			}
			if len(got.Value) != min(test.valueSize, maxValueBytes) {
				t.Fatalf("retained value size = %d, want %d", len(got.Value), min(test.valueSize, maxValueBytes))
			}
			if got.Key != "sample/key" || got.Revision != 7 || got.Version != 3 || got.Lease != 42 {
				t.Fatalf("watch metadata = %#v", got)
			}
		})
	}
}
