package substore

import (
	"encoding/json"
	"math"
	"strconv"
	"strings"

	"configflow/internal/model"
)

// Release is the selected backend asset and its public release metadata.
type Release struct {
	Version     string    `json:"version"`
	PublishedAt string    `json:"published_at"`
	ReleaseURL  string    `json:"release_url"`
	Notes       string    `json:"notes"`
	DownloadURL string    `json:"download_url"`
	Size        model.Int `json:"size"`
	SHA256      string    `json:"sha256"`
}

// UpdateState is an independently owned in-memory update observation.
type UpdateState struct {
	State         string  `json:"state"`
	Message       string  `json:"message"`
	TargetVersion string  `json:"target_version"`
	FinishedAt    float64 `json:"finished_at"`
}

// MarshalJSON encodes FinishedAt as a plain decimal floating-point token
// (0.0 when idle, 1763700000.25 after an update): Unix timestamps never use
// exponent notation, and a whole value keeps its ".0".
func (u UpdateState) MarshalJSON() ([]byte, error) {
	if math.IsNaN(u.FinishedAt) || math.IsInf(u.FinishedAt, 0) {
		return json.Marshal(struct {
			State         string  `json:"state"`
			Message       string  `json:"message"`
			TargetVersion string  `json:"target_version"`
			FinishedAt    float64 `json:"finished_at"`
		}{u.State, u.Message, u.TargetVersion, u.FinishedAt})
	}
	token := strconv.FormatFloat(u.FinishedAt, 'f', -1, 64)
	if !strings.Contains(token, ".") {
		token += ".0"
	}
	return json.Marshal(struct {
		State         string      `json:"state"`
		Message       string      `json:"message"`
		TargetVersion string      `json:"target_version"`
		FinishedAt    json.Number `json:"finished_at"`
	}{u.State, u.Message, u.TargetVersion, json.Number(token)})
}

// DependencyStatus describes the bundled converter and its current lifecycle.
type DependencyStatus struct {
	Key               string         `json:"key"`
	Name              string         `json:"name"`
	Description       string         `json:"description"`
	Homepage          string         `json:"homepage"`
	Running           bool           `json:"running"`
	Lifecycle         *RuntimeStatus `json:"lifecycle"`
	CurrentVersion    string         `json:"current_version"`
	BuiltinVersion    string         `json:"builtin_version"`
	OnlineUpdated     bool           `json:"online_updated"`
	Runtime           string         `json:"runtime"`
	LatestVersion     string         `json:"latest_version"`
	LatestPublishedAt string         `json:"latest_published_at"`
	ReleaseURL        string         `json:"release_url"`
	ReleaseNotes      string         `json:"release_notes"`
	CheckError        string         `json:"check_error"`
	HasUpdate         bool           `json:"has_update"`
	Updatable         bool           `json:"updatable"`
	Update            UpdateState    `json:"update"`
}

// ReleaseError identifies an unusable upstream release document.
type ReleaseError struct {
	Message string
	Cause   error
}

// Error returns the field or shape diagnostic.
func (e *ReleaseError) Error() string { return e.Message }

// Unwrap preserves the underlying JSON decoding error.
func (e *ReleaseError) Unwrap() error { return e.Cause }

// releasePayload is the part of a GitHub release the update check reads; the
// API's other members are ignored.
type releasePayload struct {
	TagName     string         `json:"tag_name"`
	PublishedAt string         `json:"published_at"`
	HTMLURL     string         `json:"html_url"`
	Body        string         `json:"body"`
	Assets      []releaseAsset `json:"assets"`
}

type releaseAsset struct {
	Name        string    `json:"name"`
	DownloadURL string    `json:"browser_download_url"`
	Digest      string    `json:"digest"`
	Size        model.Int `json:"size"`
}
