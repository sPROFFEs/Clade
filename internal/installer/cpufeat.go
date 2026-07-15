package installer

// CPU-feature gating for the Bun-compiled binaries PrAImate ships
// (praimate-code). Bun's default x64 builds are compiled with AVX2
// enabled; on a CPU (or VM without AVX2 passthrough) that lacks it the
// binary dies instantly with an illegal instruction — on Windows that
// surfaces as `exit status 0xc000001d` (STATUS_ILLEGAL_INSTRUCTION),
// which reads like a corrupt download. Upstream publishes "-baseline"
// (no-AVX2) variants for exactly this case; we mirror that in our
// release assets and pick the right one here.

import (
	"errors"
	"os/exec"
	"runtime"
	"strings"

	"golang.org/x/sys/cpu"
)

// statusIllegalInstruction is NTSTATUS 0xC000001D as the process exit
// code Go reports on Windows (interpreted as a signed 32-bit int).
const statusIllegalInstruction = -1073741795

// hostNeedsBaselineBuild reports whether this machine cannot run the
// default (AVX2) x64 Bun builds and needs the "-baseline" variant.
func hostNeedsBaselineBuild() bool {
	if runtime.GOARCH != "amd64" {
		return false
	}
	return !cpu.X86.HasAVX2
}

// IsIllegalInstruction reports whether err is a process failing with an
// illegal-instruction fault: exit status 0xc000001d on Windows, SIGILL
// ("signal: illegal instruction") on Unix. Used to translate the raw
// probe/install failure into "this build needs AVX2" guidance.
func IsIllegalInstruction(err error) bool {
	if err == nil {
		return false
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) && ee.ExitCode() == statusIllegalInstruction {
		return true
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "0xc000001d") || strings.Contains(s, "illegal instruction")
}
