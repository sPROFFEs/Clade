package main

// Update check — Settings-page binding over internal/updater. Check
// only: the actual swap stays with `praimate -update` (replacing a
// RUNNING gui exe mid-session is exactly the failure mode the CLI
// updater is built to sequence properly).

import (
	"github.com/sPROFFEs/PrAImate/internal/updater"
	"github.com/sPROFFEs/PrAImate/internal/version"
)

// UpdateInfo is the check result for the Settings page.
type UpdateInfo struct {
	Current   string `json:"current"`
	Latest    string `json:"latest"`
	HasUpdate bool   `json:"hasUpdate"`
	URL       string `json:"url"`
}

// CheckUpdate fetches the latest GitHub release and compares versions.
func (a *App) CheckUpdate() (*UpdateInfo, error) {
	rel, err := updater.FetchLatest()
	if err != nil {
		return nil, err
	}
	return &UpdateInfo{
		Current:   version.Current,
		Latest:    rel.TagName,
		HasUpdate: updater.IsNewer(rel.TagName, version.Current),
		URL:       rel.HTMLURL,
	}, nil
}
