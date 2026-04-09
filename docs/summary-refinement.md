# Summary Refinement Workflow

`lazy-tool-x` gets the best retrieval quality when built-in refinement is followed by a small amount of benchmark-driven manual tuning.

## Why this matters

Two tools can both mention the same product areas while serving different intents. For example:

- discovery/search: find relevant pages by topic or query
- fetch/read: retrieve the full contents of a specific page by URL

If both summaries emphasize the same domains too heavily, ranking quality drops even if embeddings are working correctly.

## Recommended workflow

1. Reindex the catalog.
2. Let built-in deterministic enrichment improve vague upstream descriptions.
3. Optionally enable `summary.provider: exec` with `summary.auto_refine: true` to refine only vague tools during reindex.
4. Run a small benchmark using realistic user queries.
5. Identify collisions or missed queries.
6. Add exact overrides only for the affected canonical names.
7. Reindex again and rerun the benchmark.

## Built-in refinement layers

`lazy-tool-x` now has two built-in layers before you reach for manual overrides:

1. Deterministic quality checks

- vagueness scoring on upstream descriptions
- schema-derived action signals and tags
- schema-enriched generated summaries for vague tools
- configurable embedding text strategy via `embeddings.text_strategy`

1. Optional `exec`-based refinement

- `summary.provider: exec`
- `summary.command` + `summary.args`
- `summary.auto_refine: true`
- only tools above `summary.vagueness_threshold` are refined

Example config:

```yaml
summary:
  provider: exec
  enabled: true
  command: opencode
  args: ["--pure", "run", "-m", "gpt-5.4-mini"]
  timeout_seconds: 120
  auto_refine: true
  vagueness_threshold: 0.5
  schema_enrichment: true

embeddings:
  text_strategy: auto
```

With `text_strategy: auto`, vague original descriptions defer to the effective summary for embedding text, so the refined description influences semantic search immediately after reindex.

## Local helper files

These files are still useful when more MCP sources are added:

- `scripts/summarize-catalog.sh`
- `scripts/summarize-catalog.overrides.json`
- `.opencode/skills/catalog-summary-refinement/SKILL.md`
- `.opencode/commands/refine-catalog-summaries.md`

The overrides file is intentionally small. It should only contain hand-tuned summaries for tools where built-in refinement plus the generic summarizer still blur intent.

## Local opencode use

This repo now includes a local `.opencode` skill and command:

- skill: `catalog-summary-refinement`
- command: `/refine-catalog-summaries`

That makes the workflow directly reusable from an opencode-enabled checkout of this repository.

## Summary style guide

- One sentence, 15-30 words
- State the primary action first
- Name the target domain or object second
- Mention key inputs only if they help distinguish the tool
- Use `[keywords: ...]` for search vocabulary that users are likely to type
- Avoid copying the original description verbatim

## Intent patterns

### Search/discovery tools

Use wording like:

- searches by topic, product, or question
- returns titles, links, excerpts, references, or guidance

### Fetch/read tools

Use wording like:

- fetches one specific URL or page
- retrieves the full article or full page content
- converts content to markdown
- used after discovery

### Code sample tools

Use wording like:

- finds examples, snippets, or implementation samples
- mention SDKs, languages, frameworks, or services when helpful

## Validation

Track at least:

- top-1 benchmark accuracy
- top-3 benchmark accuracy
- notable wins
- notable regressions

The goal is not only broader recall, but cleaner intent separation between similar tools.
