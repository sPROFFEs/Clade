# OpenCode source and attribution

PrAImate integrates an installed upstream OpenCode CLI and also publishes
**PrAImate Code**, a version-pinned and rebranded build produced from the
vendored MIT-licensed OpenCode source.

The desktop harness remains Go/Svelte and delegates model communication to the
selected CLI. PrAImate Code is a managed CLI dependency, not a replacement for
the harness core.

## Integration boundary

| Component | Responsibility |
|---|---|
| PrAImate desktop | GUI, local encrypted state, agents, skills, MCP preparation, and process launch. |
| PrAImate Code/OpenCode | Agent loop, provider request, native authentication, and native session behavior. |
| Model provider | Inference and provider-side data handling. |

PrAImate prepares configuration and launch-time environment variables. It
cannot add HTTPS when the configured model endpoint exposes only HTTP.

## Branding and distribution

The build script applies the PrAImate Code name and version pins while keeping
the upstream licensing record. Managed binaries are published separately for
Linux and Windows so the GUI can install or update the CLI without showing
duplicate product entries.

Do not remove upstream copyright or license files when refreshing the vendored
source. Review the patch set after every upstream update; branding changes and
security fixes must remain reproducible from the build script.

## Source

- Upstream repository: <https://github.com/sst/opencode>
- Upstream license: MIT
- Vendored source: `third_party/opencode/`
- Build entry point: `scripts/build-praimate-code.sh`
