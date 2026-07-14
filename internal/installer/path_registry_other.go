//go:build !windows

package installer

// ImportWindowsRegistryPath is a no-op on non-Windows. The PATH
// hydration for desktop launches is handled by ImportUserBinDirs.
func ImportWindowsRegistryPath() {}
