package mcpserver

import (
	"context"
	"errors"
	"net"
	"testing"

	"lazy-tool/internal/storage"
)

type timeoutNetErr struct{}

func (timeoutNetErr) Error() string   { return "i/o timeout" }
func (timeoutNetErr) Timeout() bool   { return true }
func (timeoutNetErr) Temporary() bool { return true }

func TestClassifyError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want ErrorClass
	}{
		{name: "nil", err: nil, want: ErrClassNone},
		{name: "deadline", err: context.DeadlineExceeded, want: ErrClassTimeout},
		{name: "net timeout", err: timeoutNetErr{}, want: ErrClassTimeout},
		{name: "network", err: errors.New("connection refused"), want: ErrClassNetwork},
		{name: "auth", err: errors.New("401 unauthorized"), want: ErrClassAuth},
		{name: "rate", err: errors.New("429 too many requests"), want: ErrClassRateLimit},
		{name: "protocol", err: errors.New("invalid request payload"), want: ErrClassProtocol},
		{name: "unknown", err: errors.New("boom"), want: ErrClassUnknown},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifyError(tc.err); got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}

func TestErrorMetadata(t *testing.T) {
	meta := errorMetadata(errors.New("401 unauthorized"))
	if meta["error_class"] != string(ErrClassAuth) {
		t.Fatalf("unexpected metadata: %#v", meta)
	}
	if errorMetadata(nil) != nil {
		t.Fatal("expected nil metadata for nil error")
	}
}

var _ net.Error = timeoutNetErr{}

func TestMergeMetadata(t *testing.T) {
	merged := mergeMetadata(map[string]any{"a": 1}, map[string]any{"error_class": string(ErrClassNetwork)})
	if merged["a"] != 1 || merged["error_class"] != string(ErrClassNetwork) {
		t.Fatalf("unexpected merged metadata: %#v", merged)
	}
	if mergeMetadata(nil, nil) != nil {
		t.Fatal("expected nil for empty metadata")
	}
	_ = storage.OperationLogEvent{}
}
