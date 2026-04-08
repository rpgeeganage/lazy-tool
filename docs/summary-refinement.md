# Summary Refinement Workflow

`lazy-tool-x` gets the best retrieval quality when generated summaries are followed by a small amount of benchmark-driven manual tuning.

## Why this matters

Two tools can both mention the same product areas while serving different intents. For example:

- discovery/search: find relevant pages by topic or query
- fetch/read: retrieve the full contents of a specific page by URL

If both summaries emphasize the same domains too heavily, ranking quality drops even if embeddings are working correctly.

## Recommended workflow

1. Reindex the catalog.
2. Generate summaries with the summarizer script.
3. Run a small benchmark using realistic user queries.
4. Identify collisions or missed queries.
5. Add exact overrides only for the affected canonical names.
6. Reindex again and rerun the benchmark.

## Local helper files

These files can be reused when more MCP sources are added:

- `scripts/summarize-catalog.sh`
- `scripts/summarize-catalog.overrides.json`
- `.opencode/skills/catalog-summary-refinement/SKILL.md`
- `.opencode/commands/refine-catalog-summaries.md`

The overrides file is intentionally small. It should only contain hand-tuned summaries for tools where the generic summarizer blurs intent.

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
