package summarizer

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"lazy-tool/internal/config"
	"lazy-tool/pkg/models"
)

func TestExecSummarizer_UsesCommandOutput(t *testing.T) {
	t.Parallel()
	rec := models.CapabilityRecord{
		Kind:                models.CapabilityKindTool,
		OriginalName:        "echo",
		OriginalDescription: "Utility tool",
	}
	s := &Exec{Command: "sh", Args: []string{"-c", "cat >/dev/null; printf 'Search repositories by owner and repo.'"}, Timeout: time.Second}
	out, err := s.Summarize(context.Background(), rec)
	if err != nil {
		t.Fatal(err)
	}
	if out != "Search repositories by owner and repo." {
		t.Fatalf("summary = %q", out)
	}
}

func TestFromConfig_ReturnsExecProvider(t *testing.T) {
	cfg := &config.Config{}
	cfg.Summary.Enabled = true
	cfg.Summary.Provider = "exec"
	cfg.Summary.Command = "opencode"
	cfg.Summary.Args = []string{"--pure"}
	cfg.Summary.TimeoutSeconds = 30
	s := FromConfig(cfg)
	execSum, ok := s.(*Exec)
	if !ok {
		t.Fatalf("expected *Exec, got %T", s)
	}
	if execSum.Command != "opencode" || len(execSum.Args) != 1 || execSum.Timeout != 30*time.Second {
		t.Fatalf("unexpected exec summarizer: %#v", execSum)
	}
}

func TestExecSummarizer_CommandRequired(t *testing.T) {
	s := &Exec{}
	_, err := s.Summarize(context.Background(), models.CapabilityRecord{})
	if err == nil || !strings.Contains(err.Error(), "command is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestExecSummarizer_WithScriptFile(t *testing.T) {
	script := filepath.Join(t.TempDir(), "summary.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\ncat >/dev/null\nprintf 'Reads repository details by id.'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	s := &Exec{Command: script, Timeout: time.Second}
	out, err := s.Summarize(context.Background(), models.CapabilityRecord{OriginalDescription: "Tool"})
	if err != nil {
		t.Fatal(err)
	}
	if out != "Reads repository details by id." {
		t.Fatalf("summary = %q", out)
	}
}
