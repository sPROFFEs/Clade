package agentic

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

type toolBroker struct {
	memory    *Memory
	artifacts artifactStore
	external  ToolExecutor
}

func (b toolBroker) execute(ctx context.Context, decision Decision) (string, *Artifact, error) {
	switch decision.Tool {
	case "memory.task":
		result, err := b.memory.add("task", decision.Arguments)
		return result, nil, err
	case "memory.note":
		result, err := b.memory.add("note", decision.Arguments)
		return result, nil, err
	case "memory.fact":
		result, err := b.memory.add("fact", decision.Arguments)
		return result, nil, err
	case "memory.decision":
		result, err := b.memory.add("decision", decision.Arguments)
		return result, nil, err
	case "artifact.write":
		artifact, err := b.artifacts.write(decision.Arguments)
		if err != nil {
			return "", nil, err
		}
		return "created artifact://" + artifact.Name, &artifact, nil
	default:
		if b.external == nil {
			return "", nil, fmt.Errorf("managed tool %q is unavailable", decision.Tool)
		}
		result, err := b.external.ExecuteTool(ctx, decision.Tool, decision.Arguments)
		return result, nil, err
	}
}

func decodeArguments(raw json.RawMessage, dst any) error {
	if len(bytes.TrimSpace(raw)) == 0 {
		return errors.New("tool arguments are required")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return fmt.Errorf("invalid tool arguments: %w", err)
	}
	return nil
}
