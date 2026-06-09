# Run a structured ScrapeGraphAI search/extraction and print JSON.
$ErrorActionPreference = "Stop"

& scrapegraph-search @args
exit $LASTEXITCODE
