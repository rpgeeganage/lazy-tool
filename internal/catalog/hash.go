package catalog

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"lazy-tool/internal/connectors"
)

func VersionHash(meta connectors.ToolMeta) string {
	h := sha256.New()
	_, _ = fmt.Fprintln(h, meta.Name)
	_, _ = fmt.Fprintln(h, meta.Description)
	h.Write(meta.InputSchema)
	h.Write(meta.AnnotationsJSON)
	return hex.EncodeToString(h.Sum(nil))
}

func VersionHashPrompt(meta connectors.PromptMeta) string {
	h := sha256.New()
	_, _ = fmt.Fprintln(h, meta.Name)
	_, _ = fmt.Fprintln(h, meta.Description)
	h.Write(meta.ArgumentsJSON)
	return hex.EncodeToString(h.Sum(nil))
}

func VersionHashResource(meta connectors.ResourceMeta) string {
	h := sha256.New()
	_, _ = fmt.Fprintln(h, meta.URI)
	_, _ = fmt.Fprintln(h, meta.Name)
	_, _ = fmt.Fprintln(h, meta.Description)
	_, _ = fmt.Fprintln(h, meta.MIMEType)
	return hex.EncodeToString(h.Sum(nil))
}

func VersionHashResourceTemplate(meta connectors.ResourceTemplateMeta) string {
	h := sha256.New()
	_, _ = fmt.Fprintln(h, meta.URITemplate)
	_, _ = fmt.Fprintln(h, meta.Name)
	_, _ = fmt.Fprintln(h, meta.Description)
	return hex.EncodeToString(h.Sum(nil))
}

func CapabilityID(sourceID, toolName string) string {
	h := sha256.Sum256([]byte(sourceID + "\x00" + toolName))
	return hex.EncodeToString(h[:])
}
