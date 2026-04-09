package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
)

type ErrorClass string

const (
	ErrClassNone      ErrorClass = ""
	ErrClassTimeout   ErrorClass = "timeout"
	ErrClassNetwork   ErrorClass = "network"
	ErrClassAuth      ErrorClass = "auth"
	ErrClassRateLimit ErrorClass = "rate_limit"
	ErrClassProtocol  ErrorClass = "protocol"
	ErrClassUnknown   ErrorClass = "unknown"
)

func classifyError(err error) ErrorClass {
	if err == nil {
		return ErrClassNone
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return ErrClassTimeout
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		if netErr.Timeout() {
			return ErrClassTimeout
		}
		return ErrClassNetwork
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "timeout"):
		return ErrClassTimeout
	case strings.Contains(msg, "connection refused"), strings.Contains(msg, "connection reset"), strings.Contains(msg, "broken pipe"), strings.Contains(msg, "no such host"):
		return ErrClassNetwork
	case strings.Contains(msg, "401"), strings.Contains(msg, "403"), strings.Contains(msg, "unauthorized"), strings.Contains(msg, "forbidden"), strings.Contains(msg, "authentication"):
		return ErrClassAuth
	case strings.Contains(msg, "429"), strings.Contains(msg, "rate limit"), strings.Contains(msg, "too many requests"):
		return ErrClassRateLimit
	case strings.Contains(msg, "invalid params"), strings.Contains(msg, "invalid request"), strings.Contains(msg, "kind="), strings.Contains(msg, "protocol"):
		return ErrClassProtocol
	default:
		return ErrClassUnknown
	}
}

func errorMetadata(err error) map[string]any {
	class := classifyError(err)
	if class == ErrClassNone {
		return nil
	}
	return map[string]any{"error_class": string(class)}
}

func mergeMetadata(base map[string]any, extra map[string]any) map[string]any {
	if len(base) == 0 && len(extra) == 0 {
		return nil
	}
	out := make(map[string]any, len(base)+len(extra))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range extra {
		out[k] = v
	}
	return out
}

func wrapClassifiedError(err error) error {
	if err == nil {
		return nil
	}
	class := classifyError(err)
	if class == ErrClassNone || class == ErrClassUnknown {
		return err
	}
	return fmt.Errorf("%s: %w", class, err)
}
