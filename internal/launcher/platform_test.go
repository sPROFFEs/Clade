package launcher

import "runtime"

func isLinux() bool   { return runtime.GOOS == "linux" }
func isWindows() bool { return runtime.GOOS == "windows" }
