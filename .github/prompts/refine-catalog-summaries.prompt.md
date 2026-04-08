# Refine Catalog Summaries

Use this workflow when adding new MCP sources or improving retrieval quality for an existing catalog.

## Goal

Produce `user_summary` text that improves search ranking and embedding quality without blurring tool intent.

## Rules

1. Keep one-sentence summaries, roughly 15-30 words.
2. Focus on user intent and distinguishing capability, not implementation details.
3. Add a `[keywords: ...]` suffix only when it materially improves retrieval.
4. Avoid repeating obvious tokens already present in the tool name.
5. For overlapping tools, sharpen the boundary explicitly:
   - search/discovery tools: topic, product, question, titles, links, excerpts
   - fetch/read tools: one known URL/page, full content, markdown, after discovery
   - code sample tools: examples, snippets, SDKs, languages, implementation
6. Prefer minimal manual overrides only for known ranking collisions.

## Recommended process

1. Reindex the source catalog.
2. Run the summarizer script.
3. Benchmark representative queries.
4. Identify ranking collisions or missed queries.
5. Hand-tune only the conflicting tools.
6. Store durable overrides for those tools.
7. Reindex and rerun the same benchmark.

## Files

- Script: `../summarize-catalog-x.sh`
- Overrides: `../summarize-catalog-x.overrides.json`

## Benchmark checklist

- Include at least 3-5 queries per important tool.
- Measure top-1 and top-3.
- Compare before and after summary changes.
- Keep a short notes table for wins, regressions, and unresolved misses.
