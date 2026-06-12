### ScrapeGraphAI Search

Use `scrapegraph-search` when web search or page extraction needs structured crawling, summarized results, or extraction from one or more URLs. The command prints JSON so it can be inspected, cited, and reused by any Clade agent CLI.

Recommended usage:

```bash
scrapegraph-search "latest docs for <topic>"
scrapegraph-search --mode api --results 5 "competitor pricing pages for <product>"
scrapegraph-search --mode oss --prompt "Extract the pricing tiers from https://example.com/pricing"
```

Use API mode when `SGAI_API_KEY` is configured. Use OSS mode for local models by setting `SCRAPEGRAPH_LLM_MODEL`, `SCRAPEGRAPH_MODEL_TOKENS`, and any search-engine key such as `SERPER_API_KEY`.

If `scrapegraph-search` is missing, ask the user to install ScrapeGraphAI from PrAImate's Tools tab or run `praimate -install-tool scrapegraph`.
