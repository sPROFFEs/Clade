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
var Current = "1.1.15"

// Repo is the GitHub slug the updater queries for releases.
const Repo = "sPROFFEs/PrAImate"

// Banner is the PRAIMATE wordmark with the monkey mascot on the left.
// Rendered on -version and mirrored by the boot splash
// (cmd/praimate/screen_splash.go keeps its own copy because it
// animates per-row; update both together).
const Banner = `   .-"-.     ██████╗ ██████╗  █████╗ ██╗███╗   ███╗ █████╗ ████████╗███████╗
  /|6 6|\    ██╔══██╗██╔══██╗██╔══██╗██║████╗ ████║██╔══██╗╚══██╔══╝██╔════╝
 {/(_0_)\}   ██████╔╝██████╔╝███████║██║██╔████╔██║███████║   ██║   █████╗
  _/ ^ \_    ██╔═══╝ ██╔══██╗██╔══██║██║██║╚██╔╝██║██╔══██║   ██║   ██╔══╝
 (/ /^\ \)   ██║     ██║  ██║██║  ██║██║██║ ╚═╝ ██║██║  ██║   ██║   ███████╗
  ""' '""    ╚═╝     ╚═╝  ╚═╝╚═╝  ╚═╝╚═╝╚═╝     ╚═╝╚═╝  ╚═╝   ╚═╝   ╚══════╝

                  one harness, every agent — shared memory & MCP`
