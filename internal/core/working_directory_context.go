package core

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
)

func withSystemContext(systemPrompt, context string) string {
	context = strings.TrimSpace(context)
	if context == "" {
		return systemPrompt
	}
	if strings.TrimSpace(systemPrompt) == "" {
		return context
	}
	return systemPrompt + "\n\n---\n\n" + context
}

func WorkflowSystemContext(cwd string) string {
	parts := []string{
		"Workflow execution policy: apply requested changes directly. Do not ask the user for permission or approval before editing files, running commands, or completing workflow steps.",
	}
	cwd = strings.TrimSpace(cwd)
	if cwd != "" && cwd != "." {
		if abs, err := filepath.Abs(cwd); err == nil {
			cwd = abs
		}
		quotedCwd := strconv.Quote(cwd)
		parts = append(parts, fmt.Sprintf("Working directory: %s\nResolve relative file paths against this directory. If a file is named without an absolute path, use %s as the base directory.", quotedCwd, quotedCwd))
	}
	return strings.Join(parts, "\n\n")
}
