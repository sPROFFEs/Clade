#!/usr/bin/env bash
# Run a structured ScrapeGraphAI search/extraction and print JSON.
set -euo pipefail

exec scrapegraph-search "$@"
