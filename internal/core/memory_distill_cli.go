package core

// CLI distiller — re-uses a registered CLIAdapter for distillation.
// The chat's own model summarises its own conversation. Useful for
// users who want the summary written in the same voice as the chat,
// and for cloud-only setups where Ollama isn't an option.
//
// The CLI distiller costs the user the same per-token rate as a regular
// chat turn. We never hide this fact — the per-chat endpoint selector
// labels this option "uses your CLI's billed model."

import (
	"context"
	"errors"
	"fmt"
	"os"
)

// cliDistiller is the concrete Distiller for DistillKindCLI.
type cliDistiller struct {
	adapter CLIAdapter
}

func newCLIDistiller(ep DistillEndpoint) (*cliDistiller, error) {
	if ep.CLIName == "" {
		return nil, errors.New("cli distiller: CLIName required")
	}
	a, err := GetCLIAdapter(ep.CLIName)
	if err != nil {
		return nil, fmt.Errorf("cli distiller: %w", err)
	}
	return &cliDistiller{adapter: a}, nil
}

func (d *cliDistiller) Name() string { return "cli:" + d.adapter.Name() }

func (d *cliDistiller) Available(ctx context.Context) error {
	return d.adapter.Available(ctx)
}

func (d *cliDistiller) Distill(ctx context.Context, messages []DistillMessage) (*DistillResult, error) {
	cwd, _ := os.Getwd()
	reply, err := d.adapter.SingleShot(ctx, SingleShotOpts{
		Cwd:     cwd,
		Message: RenderDistillInput(messages),
		// SystemPrompt deliberately empty: the prompt itself instructs
		// the model to behave as a distiller. We don't want the
		// agent's normal system prompt bleeding in and biasing the
		// output (e.g. "be terse" causing truncated summaries).
	})
	if err != nil {
		return nil, fmt.Errorf("cli distill: %w", err)
	}
	if reply.ExitCode != 0 {
		return nil, fmt.Errorf("cli distill: %s exited with code %d", d.adapter.Name(), reply.ExitCode)
	}
	return ParseDistillJSON(reply.Text)
}
