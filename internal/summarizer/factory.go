package summarizer

import (
	"os"
	"strings"
	"time"

	"lazy-tool/internal/config"
)

func FromConfig(c *config.Config) Summarizer {
	if !c.Summary.Enabled || strings.EqualFold(c.Summary.Provider, "noop") || c.Summary.Provider == "" {
		return Noop{}
	}
	if strings.Contains(strings.ToLower(c.Summary.Provider), "openai") || c.Summary.Provider == "openai-compatible" {
		key := ""
		if c.Summary.APIKeyEnv != "" {
			key = os.Getenv(c.Summary.APIKeyEnv)
		}
		return &OpenAICompatible{
			BaseURL:   c.Summary.BaseURL,
			APIKey:    key,
			Model:     c.Summary.Model,
			UserAgent: "lazy-tool/0.1",
		}
	}
	if strings.EqualFold(c.Summary.Provider, "exec") {
		return &Exec{
			Command: c.Summary.Command,
			Args:    append([]string(nil), c.Summary.Args...),
			Timeout: time.Duration(c.Summary.TimeoutSeconds) * time.Second,
		}
	}
	return Noop{}
}
