// Package version centralises the launcher's version string and ASCII
// banner so main(), the -version flag, the boot splash, and the
// updater all read from the same source.
package version

// Name is the human-facing product name. Used in TUI headers, the
// updater's user-agent, and CLI help text.
const Name = "PrAImate"

// Current is the build's semantic version. Bump on every release; the
// updater compares this against the latest GitHub release tag to decide
// whether an update is available.
//
// Declared as a var (not a const) so scripts/build.{sh,ps1} can
// override it at link time with `-ldflags "-X .../internal/version.Current=X.Y.Z"`.
// The literal here is the fallback when nothing is injected (e.g.
// `go run`, `go install`, or `go build` without our scripts).
var Current = "1.0.0"

// Repo is the GitHub slug the updater queries for releases.
const Repo = "sPROFFEs/PrAImate"

// Banner is the wordmark rendered on -version, on the boot splash, and
// at the top of README.md. Placeholder for 1.0 — final ASCII art TBD
// before the 1.0.0 release.
const Banner = `        ██████╗ ██████╗  █████╗ ██╗███╗   ███╗ █████╗ ████████╗███████╗
        ██╔══██╗██╔══██╗██╔══██╗██║████╗ ████║██╔══██╗╚══██╔══╝██╔════╝
        ██████╔╝██████╔╝███████║██║██╔████╔██║███████║   ██║   █████╗
        ██╔═══╝ ██╔══██╗██╔══██║██║██║╚██╔╝██║██╔══██║   ██║   ██╔══╝
        ██║     ██║  ██║██║  ██║██║██║ ╚═╝ ██║██║  ██║   ██║   ███████╗
        ╚═╝     ╚═╝  ╚═╝╚═╝  ╚═╝╚═╝╚═╝     ╚═╝╚═╝  ╚═╝   ╚═╝   ╚══════╝

                  one harness, every agent — shared memory & MCP`
