//go:build !windows

package main

// importWindowsRegistryPath is a no-op on non-Windows. The PATH
// propagation problem it fixes is Windows-specific.
func importWindowsRegistryPath() {}
