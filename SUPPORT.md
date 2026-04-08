# Support

## Table of contents

- [Where to start](#where-to-start)
- [Before opening an issue](#before-opening-an-issue)
- [What to include in a good bug report](#what-to-include-in-a-good-bug-report)
- [What not to do](#what-not-to-do)

## Where to start

If you need help:

1. read the root [README.md](README.md)
2. check [docs/README.md](docs/README.md)
3. check [docs/plugging-existing-mcps.md](docs/plugging-existing-mcps.md)
4. if the question is benchmark-related, check [benchmark/README.md](benchmark/README.md)

## Before opening an issue

Please gather:

- your config shape (redact secrets)
- whether the source is HTTP or stdio
- exact command or output
- `lazy-tool-x sources --status`
- `lazy-tool-x search "<query>" --limit 10`
- relevant logs or errors
- whether you ran `reindex`

## What to include in a good bug report

Strong bug reports include:

- expected behavior
- actual behavior
- reproduction steps
- environment details
- whether the issue is deterministic or flaky

## What not to do

Please do not open issues that only say:

- “doesn’t work”
- “broken”
- “bad results”

without reproduction detail.
