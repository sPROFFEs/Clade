// Package version centralises the launcher's version string and ASCII
// banner so main(), the -version flag, the boot splash, and the
// updater all read from the same source.
package version

// Current is the build's semantic version. Bump on every release; the
// updater compares this against the latest GitHub release tag to decide
// whether an update is available.
//
// Declared as a var (not a const) so scripts/build.{sh,ps1} can
// override it at link time with `-ldflags "-X .../internal/version.Current=X.Y.Z"`.
// The literal here is the fallback when nothing is injected (e.g.
// `go run`, `go install`, or `go build` without our scripts).
var Current = "0.1.8"

// Repo is the GitHub slug the updater queries for releases.
const Repo = "sPROFFEs/Clade"

// Banner is the horizontal CLADE wordmark with a cladogram on the left
// of each row. Rendered on -version, on the boot splash, and at the
// top of README.md.
const Banner = `            __         ██████╗██╗      █████╗ ██████╗ ███████╗
            / /        ██╔════╝██║     ██╔══██╗██╔══██╗██╔════╝
           / /         ██║     ██║     ███████║██║  ██║█████╗
          / / \        ██║     ██║     ██╔══██║██║  ██║██╔══╝
         / / \ \       ╚██████╗███████╗██║  ██║██████╔╝███████╗
        /_/   \_\      ╚═════╝╚══════╝╚═╝  ╚═╝╚═════╝ ╚══════╝

                           fork agent chats from one common template`
