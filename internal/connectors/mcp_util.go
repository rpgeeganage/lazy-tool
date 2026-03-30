package connectors

import (
	"context"
	"encoding/json"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func listToolsPaginated(ctx context.Context, ses *mcp.ClientSession) ([]*mcp.Tool, error) {
	var all []*mcp.Tool
	cursor := ""
	for {
		params := &mcp.ListToolsParams{}
		if cursor != "" {
			params.Cursor = cursor
		}
		res, err := ses.ListTools(ctx, params)
		if err != nil {
			return nil, err
		}
		all = append(all, res.Tools...)
		if res.NextCursor == "" {
			break
		}
		cursor = res.NextCursor
	}
	return all, nil
}

func toolsToMeta(tools []*mcp.Tool) []ToolMeta {
	out := make([]ToolMeta, 0, len(tools))
	for _, t := range tools {
		if t == nil {
			continue
		}
		var b []byte
		if t.InputSchema != nil {
			b, _ = json.Marshal(t.InputSchema)
		}
		var annJSON []byte
		if t.Annotations != nil {
			annJSON, _ = json.Marshal(t.Annotations)
		}
		out = append(out, ToolMeta{
			Name:            t.Name,
			Description:     t.Description,
			InputSchema:     b,
			AnnotationsJSON: annJSON,
		})
	}
	return out
}

func listPromptsPaginated(ctx context.Context, ses *mcp.ClientSession) ([]*mcp.Prompt, error) {
	var all []*mcp.Prompt
	for p, err := range ses.Prompts(ctx, nil) {
		if err != nil {
			return nil, err
		}
		all = append(all, p)
	}
	return all, nil
}

func listResourcesPaginated(ctx context.Context, ses *mcp.ClientSession) ([]*mcp.Resource, error) {
	var all []*mcp.Resource
	for r, err := range ses.Resources(ctx, nil) {
		if err != nil {
			return nil, err
		}
		all = append(all, r)
	}
	return all, nil
}

func listResourceTemplatesPaginated(ctx context.Context, ses *mcp.ClientSession) ([]*mcp.ResourceTemplate, error) {
	var all []*mcp.ResourceTemplate
	for rt, err := range ses.ResourceTemplates(ctx, nil) {
		if err != nil {
			return nil, err
		}
		all = append(all, rt)
	}
	return all, nil
}

func promptsToMeta(prompts []*mcp.Prompt) []PromptMeta {
	out := make([]PromptMeta, 0, len(prompts))
	for _, p := range prompts {
		if p == nil {
			continue
		}
		argsJSON, _ := json.Marshal(p.Arguments)
		out = append(out, PromptMeta{
			Name:          p.Name,
			Description:   p.Description,
			ArgumentsJSON: argsJSON,
		})
	}
	return out
}

func resourcesToMeta(resources []*mcp.Resource) []ResourceMeta {
	out := make([]ResourceMeta, 0, len(resources))
	for _, r := range resources {
		if r == nil {
			continue
		}
		out = append(out, ResourceMeta{
			URI:         r.URI,
			Name:        r.Name,
			Description: r.Description,
			MIMEType:    r.MIMEType,
		})
	}
	return out
}

func resourceTemplatesToMeta(templates []*mcp.ResourceTemplate) []ResourceTemplateMeta {
	out := make([]ResourceTemplateMeta, 0, len(templates))
	for _, t := range templates {
		if t == nil {
			continue
		}
		out = append(out, ResourceTemplateMeta{
			URITemplate: t.URITemplate,
			Name:        t.Name,
			Description: t.Description,
		})
	}
	return out
}
