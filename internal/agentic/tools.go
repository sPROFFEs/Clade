package agentic

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
)

type toolBroker struct {
	memory    *Memory
	artifacts artifactStore
}

func (b toolBroker) execute(decision Decision) (string, *Artifact, error) {
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
		return "", nil, fmt.Errorf("managed tool %q is unavailable", decision.Tool)
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
