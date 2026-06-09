# scrapegraph-search

`scrapegraph-search` is Clade's wrapper around ScrapeGraphAI packages.

Modes:

- `auto`: use ScrapeGraphAI API mode when `SGAI_API_KEY` exists, otherwise use OSS SearchGraph mode.
- `api`: require `SGAI_API_KEY` and call ScrapeGraphAI's hosted search API.
- `oss`: use the open-source `scrapegraphai` SearchGraph flow with local configuration.

Useful environment variables:

- `SGAI_API_KEY`: enables hosted API mode.
- `SCRAPEGRAPH_LLM_MODEL`: OSS/local model name, default `ollama/llama3.2`.
- `SCRAPEGRAPH_MODEL_TOKENS`: OSS/local token limit, default `8192`.
- `SCRAPEGRAPH_EMBEDDINGS_MODEL`: OSS/local embedding model, default `ollama/nomic-embed-text`.
- `SERPER_API_KEY`: optional search provider key for OSS mode.
- `SCRAPEGRAPH_SEARCH_ENGINE`: optional explicit search engine selector.

The command prints JSON. Preserve URLs and provenance from the JSON when using the results in an answer.
