package summarizer

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"lazy-tool/pkg/models"
)

type Exec struct {
	Command string
	Args    []string
	Timeout time.Duration
}

func (e *Exec) Summarize(ctx context.Context, rec models.CapabilityRecord) (string, error) {
	if strings.TrimSpace(e.Command) == "" {
		return "", fmt.Errorf("exec summarizer command is required")
	}
	timeout := e.Timeout
	if timeout <= 0 {
		timeout = 120 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, e.Command, e.Args...)
	cmd.Stdin = strings.NewReader(specSummaryPrompt(rec))
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("exec summarizer: %w", err)
	}
	text := strings.TrimSpace(string(out))
	if text == "" {
		return "", fmt.Errorf("exec summarizer returned empty output")
	}
	return enforceSummaryRules(text), nil
}
