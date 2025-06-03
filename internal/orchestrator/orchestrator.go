package orchestrator

import (
	"fmt"
	"log"
	"strings"

	"playlistporter/internal/config"
	"playlistporter/internal/models"
	"playlistporter/internal/processor"
	"playlistporter/internal/spt"
	"playlistporter/internal/tubo"
)

// Orchestrator coordinates the playlist porting workflow
type Orchestrator struct {
	cfg     *config.Config
	verbose bool

	sptClient  *spt.Client
	tuboClient *tubo.Client
	processor  *processor.Processor
}

// New creates a new Orchestrator instance
func New(cfg *config.Config, verbose bool) *Orchestrator {
	return &Orchestrator{
		cfg:     cfg,
		verbose: verbose,
	}
}

// PortPlaylist executes the complete playlist porting workflow
func (o *Orchestrator) PortPlaylist(sptURL string) error {
	o.logf("Starting playlist porting from: %s", sptURL)

	// Step 1: Initialize clients
	if err := o.initializeClients(); err != nil {
		return fmt.Errorf("initializing clients: %w", err)
	}

	// Step 2: Extract playlist ID from URL
	playlistID, err := o.extractPlaylistID(sptURL)
	if err != nil {
		return fmt.Errorf("extracting playlist ID: %w", err)
	}

	// Step 3: Fetch playlist from SPT
	o.logf("Fetching playlist from SPT...")
	playlist, err := o.sptClient.GetPlaylist(playlistID)
	if err != nil {
		return fmt.Errorf("fetching SPT playlist: %w", err)
	}
	o.logf("Found playlist: %s (%d tracks)", playlist.Name, len(playlist.Tracks))

	// Step 4: Process and normalize track data
	o.logf("Processing track metadata...")
	o.processor.NormalizePlaylist(playlist)

	// Step 5: Search and match tracks on TUBO
	o.logf("Searching for tracks on TUBO Music...")
	matchResults, err := o.matchTracks(playlist.Tracks)
	if err != nil {
		return fmt.Errorf("matching tracks: %w", err)
	}

	// Step 6: Create playlist on TUBO
	o.logf("Creating playlist on TUBO Music...")
	tuboPlaylist, err := o.createTUBOPlaylist(playlist, matchResults)
	if err != nil {
		return fmt.Errorf("creating TUBO playlist: %w", err)
	}

	// Step 7: Report results
	o.reportResults(playlist, tuboPlaylist, matchResults)

	return nil
}

// initializeClients sets up all required service clients
func (o *Orchestrator) initializeClients() error {
	o.logf("🔧 Initializing service clients...")

	// Initialize SPT client
	sptClient, err := spt.NewClient(&o.cfg.SPT)
	if err != nil {
		return fmt.Errorf("creating SPT client: %w", err)
	}
	o.sptClient = sptClient

	// Initialize TUBO client
	tuboClient, err := tubo.NewClient(&o.cfg.TUBO)
	if err != nil {
		return fmt.Errorf("creating TUBO client: %w", err)
	}
	o.tuboClient = tuboClient

	// Initialize processor
	o.processor = processor.New()

	return nil
}

// extractPlaylistID extracts playlist ID from SPT URL
func (o *Orchestrator) extractPlaylistID(url string) (string, error) {
	// Expected format: https://open.spt.com/playlist/37i9dQZF1DXcBWIGoYBM5M?si=...
	parts := strings.Split(url, "/")
	if len(parts) < 2 {
		return "", fmt.Errorf("invalid SPT URL format")
	}

	for i, part := range parts {
		if part == "playlist" && i+1 < len(parts) {
			playlistID := parts[i+1]
			// Remove query parameters if present
			if idx := strings.Index(playlistID, "?"); idx != -1 {
				playlistID = playlistID[:idx]
			}
			return playlistID, nil
		}
	}

	return "", fmt.Errorf("playlist ID not found in URL")
}

// matchTracks searches for each track on TUBO Music
func (o *Orchestrator) matchTracks(tracks []models.Track) ([]models.MatchResult, error) {
	results := make([]models.MatchResult, 0, len(tracks))

	for i, track := range tracks {
		o.logf("Matching track %d/%d: %s - %s", i+1, len(tracks), track.Artist, track.Title)

		matchedTrack, score, err := o.tuboClient.SearchTrack(track)
		if err != nil {
			o.logf("Failed to search track: %v", err)
			results = append(results, models.MatchResult{
				OriginalTrack: track,
				Matched:       false,
				Error:         err.Error(),
			})
			continue
		}

		if matchedTrack != nil {
			o.logf("Found match (score: %.2f): %s - %s", score, matchedTrack.Artist, matchedTrack.Title)
			results = append(results, models.MatchResult{
				OriginalTrack: track,
				MatchedTrack:  matchedTrack,
				MatchScore:    score,
				Matched:       true,
			})
		} else {
			o.logf("No match found for: %s - %s", track.Artist, track.Title)
			results = append(results, models.MatchResult{
				OriginalTrack: track,
				Matched:       false,
			})
		}
	}

	return results, nil
}

// createTUBOPlaylist creates the playlist on TUBO Music
func (o *Orchestrator) createTUBOPlaylist(originalPlaylist *models.Playlist, matchResults []models.MatchResult) (*models.Playlist, error) {
	// Create empty playlist
	tuboPlaylist, err := o.tuboClient.CreatePlaylist(originalPlaylist.Name, originalPlaylist.Description)
	if err != nil {
		return nil, fmt.Errorf("creating playlist: %w", err)
	}

	// Add matched tracks
	var trackIDs []string
	for _, result := range matchResults {
		if result.Matched && result.MatchedTrack != nil {
			trackIDs = append(trackIDs, result.MatchedTrack.ID)
		}
	}

	if len(trackIDs) > 0 {
		if err := o.tuboClient.AddTracksToPlaylist(tuboPlaylist.ID, trackIDs); err != nil {
			return nil, fmt.Errorf("adding tracks to playlist: %w", err)
		}
	}

	return tuboPlaylist, nil
}

// reportResults prints a summary of the porting operation
func (o *Orchestrator) reportResults(original *models.Playlist, created *models.Playlist, results []models.MatchResult) {
	successful := 0
	failed := 0

	for _, result := range results {
		if result.Matched {
			successful++
		} else {
			failed++
		}
	}

	fmt.Println("\n PORTING RESULTS")
	fmt.Println("==================")
	fmt.Printf("Original playlist: %s\n", original.Name)
	fmt.Printf("Total tracks: %d\n", len(original.Tracks))
	fmt.Printf("Successfully matched: %d\n", successful)
	fmt.Printf("Failed to match: %d\n", failed)
	fmt.Printf("Success rate: %.1f%%\n", float64(successful)/float64(len(original.Tracks))*100)

	if created != nil {
		fmt.Printf("Created playlist ID: %s\n", created.ID)
	}
}

// logf prints a log message if verbose mode is enabled
func (o *Orchestrator) logf(format string, args ...interface{}) {
	if o.verbose {
		log.Printf(format, args...)
	}
}
