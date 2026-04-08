---
name: catalog-summary-refinement
description: "Benchmark-driven workflow for refining MCP catalog user summaries, preserving intent boundaries between search, fetch, and code-sample tools, and storing durable overrides for ranking collisions."
---

# Catalog Summary Refinement

Use this skill when adding MCP sources or improving retrieval quality for an existing `lazy-tool-x` catalog.

## Goal

Produce `user_summary` text that improves lexical search and embeddings without blurring tool intent.

## Core rules

1. Keep summaries to one sentence, roughly 15-30 words.
2. Focus on user intent and distinguishing capability.
3. Add `[keywords: ...]` only when it materially improves retrieval.
4. Avoid repeating obvious words already in the tool name.
5. For overlapping tools, sharpen the boundary explicitly:
   - search/discovery: topic, product, question, titles, links, excerpts, guidance
   - fetch/read: one known URL or page, full article, markdown, after discovery
   - code sample: examples, snippets, SDKs, implementation, languages
6. Use exact manual overrides only for known ranking collisions.

## Recommended process

1. Reindex the catalog.
2. Generate summaries with the summarizer script.
3. Benchmark representative queries.
4. Identify ranking collisions or misses.
5. Hand-tune only the conflicting tools.
6. Store durable overrides for those tools.
7. Reindex and rerun the same benchmark.

## Files in this repo

- `scripts/summarize-catalog.sh`
- `scripts/summarize-catalog.overrides.json`
- `docs/summary-refinement.md`

## Benchmark checklist

- Include at least 3-5 queries per important tool.
- Measure top-1 and top-3.
- Compare before and after summary changes.
- Keep a short notes table for wins, regressions, and unresolved misses.
