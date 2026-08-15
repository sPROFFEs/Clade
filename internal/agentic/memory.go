package agentic

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type MemoryItem struct {
	Kind      string    `json:"kind"`
	Title     string    `json:"title,omitempty"`
	Content   string    `json:"content"`
	Status    string    `json:"status,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

type Memory struct {
	Items []MemoryItem `json:"items"`
}

func (m *Memory) add(kind string, raw json.RawMessage) (string, error) {
	var args struct {
		Title   string `json:"title"`
		Content string `json:"content"`
		Status  string `json:"status"`
	}
	if err := decodeArguments(raw, &args); err != nil {
		return "", err
	}
	args.Title = strings.TrimSpace(args.Title)
	args.Content = strings.TrimSpace(args.Content)
	args.Status = strings.TrimSpace(args.Status)
	if args.Content == "" && args.Title == "" {
		return "", fmt.Errorf("%s requires title or content", kind)
	}
	if kind == "task" && args.Status == "" {
		args.Status = "pending"
	}
	m.Items = append(m.Items, MemoryItem{
		Kind: kind, Title: args.Title, Content: args.Content,
		Status: args.Status, CreatedAt: time.Now().UTC(),
	})
	return fmt.Sprintf("stored %s %d", kind, len(m.Items)), nil
}

func (m Memory) summary(maxChars int) string {
	if len(m.Items) == 0 {
		return "No working-memory items yet."
	}
	selected := make([]string, 0, len(m.Items))
	used := 0
	for i := len(m.Items) - 1; i >= 0; i-- {
		item := m.Items[i]
		line := fmt.Sprintf("%d. [%s]", i+1, item.Kind)
		if item.Status != "" {
			line += "[" + item.Status + "]"
		}
		if item.Title != "" {
			line += " " + item.Title
		}
		if item.Content != "" {
			line += ": " + item.Content
		}
		line += "\n"
		if used+len(line) > maxChars {
			break
		}
		selected = append(selected, line)
		used += len(line)
	}
	var b strings.Builder
	if len(selected) < len(m.Items) {
		b.WriteString("… older working-memory items omitted …\n")
	}
	for i := len(selected) - 1; i >= 0; i-- {
		b.WriteString(selected[i])
	}
	return b.String()
}
