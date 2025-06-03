package models

import "time"

// Track represents a music track with metadata
type Track struct {
	ID          string        `json:"id"`
	Title       string        `json:"title"`
	Artist      string        `json:"artist"`
	Album       string        `json:"album"`
	Duration    time.Duration `json:"duration"`
	ReleaseYear int           `json:"release_year,omitempty"`
	ISRC        string        `json:"isrc,omitempty"` // International Standard Recording Code

	// Normalized versions for better matching
	NormalizedTitle  string `json:"normalized_title"`
	NormalizedArtist string `json:"normalized_artist"`
}

// Playlist represents a music playlist
type Playlist struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Description string  `json:"description,omitempty"`
	Tracks      []Track `json:"tracks"`
	TotalTracks int     `json:"total_tracks"`
	IsPublic    bool    `json:"is_public"`
	OwnerID     string  `json:"owner_id,omitempty"`
}

// MatchResult represents the result of matching a track
type MatchResult struct {
	OriginalTrack Track   `json:"original_track"`
	MatchedTrack  *Track  `json:"matched_track,omitempty"`
	MatchScore    float64 `json:"match_score"` // 0.0 to 1.0
	Matched       bool    `json:"matched"`
	Error         string  `json:"error,omitempty"`
}

// PortingResult represents the final result of the porting operation
type PortingResult struct {
	SourcePlaylist    Playlist      `json:"source_playlist"`
	CreatedPlaylist   *Playlist     `json:"created_playlist,omitempty"`
	MatchResults      []MatchResult `json:"match_results"`
	SuccessfulMatches int           `json:"successful_matches"`
	FailedMatches     int           `json:"failed_matches"`
	TotalTracks       int           `json:"total_tracks"`
	Success           bool          `json:"success"`
	Error             string        `json:"error,omitempty"`
}
