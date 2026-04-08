---
description: Refine lazy-tool-x catalog summaries using the local summarizer, benchmark representative queries, and preserve exact overrides for ranking collisions.
---

Refine `lazy-tool-x` catalog summaries for better retrieval quality.

## Usage

```text
/refine-catalog-summaries [optional-canonical-name]
```

## Instructions

1. Load the `catalog-summary-refinement` skill for the workflow rules.
2. If an argument is provided, treat it as one canonical capability name to inspect and refine.
3. If no argument is provided, work on the current catalog broadly.
4. Read:
   - `docs/summary-refinement.md`
   - `scripts/summarize-catalog.sh`
   - `scripts/summarize-catalog.overrides.json` if present
5. Preserve the distinction between discovery/search, full-page fetch, and code-sample tools.
6. Prefer minimal exact overrides only for tools with demonstrated ranking collisions.
7. After summary changes, reindex and compare representative queries before declaring success.
8. Summarize the benchmark deltas clearly.
