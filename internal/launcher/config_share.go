package launcher

// Shareable-config slice for the cross-host backup. Only fields that
// make sense on EVERY machine travel: the local-LLM endpoint defaults
// and the last-used agent. Host-specific fields (WorkspacesRoot,
// BackupMachineID, the backup toggles themselves) stay local — syncing
// those would point machine B at machine A's filesystem or defeat the
// per-machine force-push guard.

import "encoding/json"

// ShareableConfig is the subset of Config that syncs across machines
// via the backup repo's .praimate-state/config.json.
type ShareableConfig struct {
	LastAgent            string `json:"lastAgent,omitempty"`
	DefaultLocalEndpoint string `json:"defaultLocalEndpoint,omitempty"`
	// Kept only so older remote files decode cleanly. Secrets are local-only
	// and never exported or applied.
	DefaultLocalAPIKey        string `json:"defaultLocalApiKey,omitempty"`
	DefaultLocalWireAPI       string `json:"defaultLocalWireApi,omitempty"`
	DefaultLocalContextTokens int    `json:"defaultLocalContextTokens,omitempty"`
	DefaultLocalOutputTokens  int    `json:"defaultLocalOutputTokens,omitempty"`
}

// ShareableConfigJSON loads the current config and serialises its
// shareable slice. Returns (nil, nil) when no config exists yet.
func ShareableConfigJSON() ([]byte, error) {
	cfg, err := LoadConfig()
	if err != nil || cfg == nil {
		return nil, err
	}
	s := ShareableConfig{
		LastAgent:                 cfg.LastAgent,
		DefaultLocalEndpoint:      cfg.DefaultLocalEndpoint,
		DefaultLocalWireAPI:       cfg.DefaultLocalWireAPI,
		DefaultLocalContextTokens: cfg.DefaultLocalContextTokens,
		DefaultLocalOutputTokens:  cfg.DefaultLocalOutputTokens,
	}
	raw, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(raw, '\n'), nil
}

// ApplyShareableConfig merges a remote machine's shareable slice into
// the local config and saves it. Empty/zero remote fields never clear a
// locally-set value (a host that never configured a local endpoint must
// not wipe one that did). No-op when raw is empty.
func ApplyShareableConfig(raw []byte) error {
	if len(raw) == 0 {
		return nil
	}
	var s ShareableConfig
	if err := json.Unmarshal(raw, &s); err != nil {
		return err
	}
	cfg, err := LoadConfig()
	if err != nil {
		return err
	}
	if cfg == nil {
		cfg = &Config{}
	}
	changed := false
	apply := func(dst *string, src string) {
		if src != "" && *dst != src {
			*dst = src
			changed = true
		}
	}
	apply(&cfg.LastAgent, s.LastAgent)
	apply(&cfg.DefaultLocalEndpoint, s.DefaultLocalEndpoint)
	apply(&cfg.DefaultLocalWireAPI, s.DefaultLocalWireAPI)
	if s.DefaultLocalContextTokens != 0 && cfg.DefaultLocalContextTokens != s.DefaultLocalContextTokens {
		cfg.DefaultLocalContextTokens = s.DefaultLocalContextTokens
		changed = true
	}
	if s.DefaultLocalOutputTokens != 0 && cfg.DefaultLocalOutputTokens != s.DefaultLocalOutputTokens {
		cfg.DefaultLocalOutputTokens = s.DefaultLocalOutputTokens
		changed = true
	}
	if !changed {
		return nil
	}
	return SaveConfig(cfg)
}
