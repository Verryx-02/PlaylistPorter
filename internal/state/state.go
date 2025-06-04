package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"playlistporter/internal/models"
)

// PortingState represents the persistent state of a porting operation
type PortingState struct {
	// Metadata
	Version       string    `json:"version"`
	CreatedAt     time.Time `json:"created_at"`
	LastUpdatedAt time.Time `json:"last_updated_at"`
	SpotifyURL    string    `json:"spotify_url"`
	SpotifyID     string    `json:"spotify_id"`

	// Original playlist info
	OriginalPlaylist models.Playlist `json:"original_playlist"`

	// Progress tracking
	ProcessedTracks int  `json:"processed_tracks"`
	TotalTracks     int  `json:"total_tracks"`
	IsComplete      bool `json:"is_complete"`

	// Match results for all processed tracks
	MatchResults []models.MatchResult `json:"match_results"`

	// YouTube playlist info (if created)
	YouTubePlaylistID   string `json:"youtube_playlist_id,omitempty"`
	YouTubePlaylistName string `json:"youtube_playlist_name,omitempty"`

	// Session history
	Sessions []SessionInfo `json:"sessions"`

	// Sync tracking
	LastSyncCheck     time.Time       `json:"last_sync_check,omitempty"`
	ProcessedTrackIDs map[string]bool `json:"processed_track_ids"` // Track Spotify IDs already processed
}

// SessionInfo tracks information about each processing session
type SessionInfo struct {
	StartTime       time.Time `json:"start_time"`
	EndTime         time.Time `json:"end_time"`
	TracksProcessed int       `json:"tracks_processed"`
	TracksMatched   int       `json:"tracks_matched"`
	QuotaUsed       int       `json:"quota_used_estimate"` // Rough estimate
}

// Manager handles state persistence
type Manager struct {
	stateDir string
}

// NewManager creates a new state manager
func NewManager(stateDir string) (*Manager, error) {
	// Create state directory if it doesn't exist
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		return nil, fmt.Errorf("creating state directory: %w", err)
	}

	return &Manager{
		stateDir: stateDir,
	}, nil
}

// GetStateFilePath returns the path to the state file for a given Spotify playlist ID
func (m *Manager) GetStateFilePath(spotifyID string) string {
	filename := fmt.Sprintf("playlist_%s_state.json", spotifyID)
	return filepath.Join(m.stateDir, filename)
}

// LoadState loads the state for a given Spotify playlist ID
func (m *Manager) LoadState(spotifyID string) (*PortingState, error) {
	statePath := m.GetStateFilePath(spotifyID)

	data, err := os.ReadFile(statePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // No state exists yet
		}
		return nil, fmt.Errorf("reading state file: %w", err)
	}

	var state PortingState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("parsing state file: %w", err)
	}

	// Validate state version
	if state.Version != "1.0" {
		return nil, fmt.Errorf("unsupported state version: %s", state.Version)
	}

	return &state, nil
}

// SaveState saves the current state
func (m *Manager) SaveState(state *PortingState) error {
	state.LastUpdatedAt = time.Now()

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling state: %w", err)
	}

	statePath := m.GetStateFilePath(state.SpotifyID)

	// Write to temporary file first, then rename (atomic operation)
	tmpPath := statePath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("writing state file: %w", err)
	}

	if err := os.Rename(tmpPath, statePath); err != nil {
		os.Remove(tmpPath) // Clean up temp file
		return fmt.Errorf("renaming state file: %w", err)
	}

	return nil
}

// CreateNewState creates a new state for a playlist
func (m *Manager) CreateNewState(spotifyURL, spotifyID string, playlist models.Playlist) *PortingState {
	// Initialize processed track IDs map
	processedIDs := make(map[string]bool)

	return &PortingState{
		Version:           "1.0",
		CreatedAt:         time.Now(),
		LastUpdatedAt:     time.Now(),
		SpotifyURL:        spotifyURL,
		SpotifyID:         spotifyID,
		OriginalPlaylist:  playlist,
		ProcessedTracks:   0,
		TotalTracks:       len(playlist.Tracks),
		IsComplete:        false,
		MatchResults:      make([]models.MatchResult, 0, len(playlist.Tracks)),
		Sessions:          []SessionInfo{},
		ProcessedTrackIDs: processedIDs,
	}
}

// GetUnprocessedTracks returns the tracks that haven't been processed yet
func (s *PortingState) GetUnprocessedTracks() []models.Track {
	var unprocessedTracks []models.Track

	// If we have a processed IDs map, use it (more accurate for sync scenarios)
	if s.ProcessedTrackIDs != nil {
		for _, track := range s.OriginalPlaylist.Tracks {
			if !s.ProcessedTrackIDs[track.ID] {
				unprocessedTracks = append(unprocessedTracks, track)
			}
		}
		return unprocessedTracks
	}

	// Fallback to index-based approach
	if s.ProcessedTracks >= len(s.OriginalPlaylist.Tracks) {
		return []models.Track{}
	}

	return s.OriginalPlaylist.Tracks[s.ProcessedTracks:]
}

// GetNextBatch returns the next batch of tracks to process
func (s *PortingState) GetNextBatch(batchSize int) []models.Track {
	unprocessed := s.GetUnprocessedTracks()

	if len(unprocessed) <= batchSize {
		return unprocessed
	}

	return unprocessed[:batchSize]
}

// AddMatchResults adds new match results and updates progress
func (s *PortingState) AddMatchResults(results []models.MatchResult) {
	s.MatchResults = append(s.MatchResults, results...)
	s.ProcessedTracks = len(s.MatchResults)

	// Update processed track IDs map
	if s.ProcessedTrackIDs == nil {
		s.ProcessedTrackIDs = make(map[string]bool)
	}

	for _, result := range results {
		s.ProcessedTrackIDs[result.OriginalTrack.ID] = true
	}

	if s.ProcessedTracks >= s.TotalTracks {
		s.IsComplete = true
	}
}

// DetectNewTracks compares current playlist with saved state to find new tracks
func (s *PortingState) DetectNewTracks(currentPlaylist models.Playlist) []models.Track {
	var newTracks []models.Track

	// Initialize map if it doesn't exist (for backward compatibility)
	if s.ProcessedTrackIDs == nil {
		s.ProcessedTrackIDs = make(map[string]bool)
		// Populate from existing match results
		for _, result := range s.MatchResults {
			s.ProcessedTrackIDs[result.OriginalTrack.ID] = true
		}
	}

	// Find tracks that aren't in our processed set
	for _, track := range currentPlaylist.Tracks {
		if !s.ProcessedTrackIDs[track.ID] {
			newTracks = append(newTracks, track)
		}
	}

	return newTracks
}

// UpdateForSync updates the state when syncing with an updated playlist
func (s *PortingState) UpdateForSync(currentPlaylist models.Playlist) {
	// Update the original playlist info
	s.OriginalPlaylist.Name = currentPlaylist.Name
	s.OriginalPlaylist.Description = currentPlaylist.Description
	s.OriginalPlaylist.TotalTracks = currentPlaylist.TotalTracks

	// Update total tracks count
	s.TotalTracks = len(currentPlaylist.Tracks)

	// Mark as incomplete if there are new tracks
	if s.ProcessedTracks < s.TotalTracks {
		s.IsComplete = false
	}

	// Update last sync check time
	s.LastSyncCheck = time.Now()
}

// GetProcessedTrackCount returns the actual number of unique tracks processed
func (s *PortingState) GetProcessedTrackCount() int {
	if s.ProcessedTrackIDs == nil {
		return s.ProcessedTracks
	}
	return len(s.ProcessedTrackIDs)
}

// NeedsMigration checks if the state needs migration to support new features
func (s *PortingState) NeedsMigration() bool {
	return s.ProcessedTrackIDs == nil && len(s.MatchResults) > 0
}

// Migrate updates old state files to support new features
func (s *PortingState) Migrate() {
	if s.ProcessedTrackIDs == nil {
		s.ProcessedTrackIDs = make(map[string]bool)
		for _, result := range s.MatchResults {
			s.ProcessedTrackIDs[result.OriginalTrack.ID] = true
		}
	}
}

// StartNewSession starts tracking a new processing session
func (s *PortingState) StartNewSession() {
	session := SessionInfo{
		StartTime: time.Now(),
	}
	s.Sessions = append(s.Sessions, session)
}

// EndCurrentSession ends the current session with statistics
func (s *PortingState) EndCurrentSession(tracksProcessed, tracksMatched int) {
	if len(s.Sessions) == 0 {
		return
	}

	// Update the last session
	lastIdx := len(s.Sessions) - 1
	s.Sessions[lastIdx].EndTime = time.Now()
	s.Sessions[lastIdx].TracksProcessed = tracksProcessed
	s.Sessions[lastIdx].TracksMatched = tracksMatched
	// Rough estimate: 100 quota units per search, assume 2 searches per track average
	s.Sessions[lastIdx].QuotaUsed = tracksProcessed * 200
}

// GetProgress returns a human-readable progress string
func (s *PortingState) GetProgress() string {
	percentage := float64(s.ProcessedTracks) / float64(s.TotalTracks) * 100
	return fmt.Sprintf("%d/%d tracks (%.1f%%)", s.ProcessedTracks, s.TotalTracks, percentage)
}

// GetTotalQuotaUsed estimates total quota used across all sessions
func (s *PortingState) GetTotalQuotaUsed() int {
	total := 0
	for _, session := range s.Sessions {
		total += session.QuotaUsed
	}
	return total
}

// ListStates lists all saved states in the state directory
func (m *Manager) ListStates() ([]string, error) {
	entries, err := os.ReadDir(m.stateDir)
	if err != nil {
		return nil, fmt.Errorf("reading state directory: %w", err)
	}

	var states []string
	for _, entry := range entries {
		if !entry.IsDir() && filepath.Ext(entry.Name()) == ".json" {
			states = append(states, entry.Name())
		}
	}

	return states, nil
}
