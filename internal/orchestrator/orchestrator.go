package orchestrator

import (
	"fmt"
	"log"
	"os"
	"strings"
	"time"

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
	logFile *os.File
	logger  *log.Logger

	sptClient  *spt.Client
	tuboClient *tubo.Client
	processor  *processor.Processor
}

// New creates a new Orchestrator instance with optional log file
func New(cfg *config.Config, verbose bool, logFilePath string) *Orchestrator {
	orch := &Orchestrator{
		cfg:     cfg,
		verbose: verbose,
	}

	// Setup file logging if verbose mode is enabled
	if verbose && logFilePath != "" {
		logFile, err := os.Create(logFilePath)
		if err != nil {
			log.Printf("Warning: Failed to create log file %s: %v", logFilePath, err)
		} else {
			orch.logFile = logFile
			orch.logger = log.New(logFile, "", log.LstdFlags)
			orch.writeToLog("=== PlaylistPorter Detailed Log ===")
			orch.writeToLog("Started at: %s", time.Now().Format("2006-01-02 15:04:05"))
		}
	}

	return orch
}

// Close closes the log file if it's open
func (o *Orchestrator) Close() {
	if o.logFile != nil {
		o.writeToLog("=== Session ended at: %s ===", time.Now().Format("2006-01-02 15:04:05"))
		o.logFile.Close()
	}
}

// writeToLog writes a message to the log file
func (o *Orchestrator) writeToLog(format string, args ...interface{}) {
	if o.logger != nil {
		o.logger.Printf(format, args...)
	}
}

// PortPlaylist executes the complete playlist porting workflow
func (o *Orchestrator) PortPlaylist(sptURL string) error {
	defer o.Close() // Ensure log file is closed when done

	o.writeToLog("Starting playlist porting from: %s", sptURL)

	// Step 1: Initialize clients
	if err := o.initializeClients(); err != nil {
		return fmt.Errorf("initializing clients: %w", err)
	}

	// Step 2: Extract playlist ID from URL
	playlistID, err := o.extractPlaylistID(sptURL)
	if err != nil {
		return fmt.Errorf("extracting playlist ID: %w", err)
	}
	o.writeToLog("Extracted playlist ID: %s", playlistID)

	// Step 3: Fetch playlist from SPT
	fmt.Printf("🎵 Fetching playlist from Spotify...\n")
	o.writeToLog("Fetching playlist from SPT...")

	playlist, err := o.sptClient.GetPlaylist(playlistID)
	if err != nil {
		return fmt.Errorf("fetching SPT playlist: %w", err)
	}

	fmt.Printf("📋 Found playlist: \"%s\" (%d tracks)\n", playlist.Name, len(playlist.Tracks))
	o.writeToLog("Found playlist: %s (%d tracks)", playlist.Name, len(playlist.Tracks))

	// Log all tracks for reference
	o.writeToLog("\n=== ORIGINAL TRACKS ===")
	for i, track := range playlist.Tracks {
		o.writeToLog("%d. \"%s\" by \"%s\" [%s]", i+1, track.Title, track.Artist, track.Album)
	}

	// Step 4: Process and normalize track data
	fmt.Printf("🔧 Processing track metadata...\n")
	o.writeToLog("\n=== NORMALIZING METADATA ===")
	o.processor.NormalizePlaylist(playlist)

	// Log normalized tracks
	for i, track := range playlist.Tracks {
		o.writeToLog("%d. Original: \"%s\" by \"%s\"", i+1, track.Title, track.Artist)
		o.writeToLog("   Normalized: \"%s\" by \"%s\"", track.NormalizedTitle, track.NormalizedArtist)
	}

	// Step 5: Search and match tracks on YouTube
	fmt.Printf("🔍 Searching for tracks on YouTube...\n")
	if o.verbose {
		fmt.Printf("    💡 Detailed search progress is being logged to file\n")
	}

	o.writeToLog("\n=== YOUTUBE SEARCH & MATCHING ===")
	matchResults, err := o.matchTracks(playlist.Tracks)
	if err != nil {
		return fmt.Errorf("matching tracks: %w", err)
	}

	// Step 6: Create playlist on YouTube
	fmt.Printf("📝 Creating playlist on YouTube...\n")
	o.writeToLog("\n=== CREATING YOUTUBE PLAYLIST ===")

	tuboPlaylist, err := o.createTUBOPlaylist(playlist, matchResults)
	if err != nil {
		return fmt.Errorf("creating TUBO playlist: %w", err)
	}
	o.writeToLog("Created playlist with ID: %s", tuboPlaylist.ID)

	// Step 7: Report results
	o.reportResults(playlist, tuboPlaylist, matchResults)

	return nil
}

// initializeClients sets up all required service clients
func (o *Orchestrator) initializeClients() error {
	o.writeToLog("🔧 Initializing service clients...")

	// Initialize SPT client
	sptClient, err := spt.NewClient(&o.cfg.SPT)
	if err != nil {
		return fmt.Errorf("creating SPT client: %w", err)
	}
	o.sptClient = sptClient
	o.writeToLog("✅ Spotify client initialized")

	// Initialize TUBO client
	tuboClient, err := tubo.NewClient(&o.cfg.TUBO)
	if err != nil {
		return fmt.Errorf("creating TUBO client: %w", err)
	}

	// Pass logger to TUBO client for detailed logging
	if o.logger != nil {
		tuboClient.SetLogger(o.logger)
	}
	tuboClient.SetVerbose(o.verbose)
	o.tuboClient = tuboClient
	o.writeToLog("✅ YouTube client initialized")

	// Initialize processor
	o.processor = processor.New()
	o.writeToLog("✅ Processor initialized")

	return nil
}

// extractPlaylistID extracts playlist ID from SPT URL
func (o *Orchestrator) extractPlaylistID(url string) (string, error) {
	// Expected format: https://open.spotify.com/playlist/37i9dQZF1DXcBWIGoYBM5M?si=...
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

// matchTracks searches for each track on YouTube
func (o *Orchestrator) matchTracks(tracks []models.Track) ([]models.MatchResult, error) {
	results := make([]models.MatchResult, 0, len(tracks))

	for i, track := range tracks {
		// Show progress in terminal (clean)
		fmt.Printf("\r🎵 Matching tracks: %d/%d (%d%%) - %s",
			i+1, len(tracks),
			(i+1)*100/len(tracks),
			truncateString(fmt.Sprintf("%s - %s", track.Artist, track.Title), 40))

		// Detailed logging to file
		o.writeToLog("\n--- TRACK %d/%d ---", i+1, len(tracks))
		o.writeToLog("Searching for: \"%s\" by \"%s\"", track.Title, track.Artist)
		o.writeToLog("Normalized: \"%s\" by \"%s\"", track.NormalizedTitle, track.NormalizedArtist)

		matchedTrack, score, err := o.tuboClient.SearchTrack(track)
		if err != nil {
			o.writeToLog("❌ Search error: %v", err)
			results = append(results, models.MatchResult{
				OriginalTrack: track,
				Matched:       false,
				Error:         err.Error(),
			})
			continue
		}

		if matchedTrack != nil {
			o.writeToLog("✅ MATCH FOUND (score: %.2f)", score)
			o.writeToLog("   YouTube: \"%s\" by \"%s\"", matchedTrack.Title, matchedTrack.Artist)
			o.writeToLog("   Video ID: %s", matchedTrack.ID)

			results = append(results, models.MatchResult{
				OriginalTrack: track,
				MatchedTrack:  matchedTrack,
				MatchScore:    score,
				Matched:       true,
			})
		} else {
			o.writeToLog("❌ NO MATCH FOUND")
			results = append(results, models.MatchResult{
				OriginalTrack: track,
				Matched:       false,
			})
		}
	}

	// Clear progress line
	fmt.Printf("\r🎵 Matching complete!                                        \n")

	return results, nil
}

// createTUBOPlaylist creates the playlist on YouTube
func (o *Orchestrator) createTUBOPlaylist(originalPlaylist *models.Playlist, matchResults []models.MatchResult) (*models.Playlist, error) {
	// Use completely different name to avoid any cache issues
	timestamp := time.Now().Format("2006-01-02 15:04")
	playlistName := fmt.Sprintf("PlaylistPorter Test %s", timestamp)
	description := fmt.Sprintf("Migrated from Spotify playlist: %s", originalPlaylist.Name)

	fmt.Printf("🔧 Creating playlist with test name: \"%s\"\n", playlistName)

	// Create empty playlist
	tuboPlaylist, err := o.tuboClient.CreatePlaylist(playlistName, description)
	if err != nil {
		return nil, fmt.Errorf("creating playlist: %w", err)
	}
	o.writeToLog("Created empty playlist: %s", tuboPlaylist.Name)

	// Add matched tracks
	var trackIDs []string
	for _, result := range matchResults {
		if result.Matched && result.MatchedTrack != nil {
			trackIDs = append(trackIDs, result.MatchedTrack.ID)
		}
	}

	o.writeToLog("Adding %d tracks to playlist...", len(trackIDs))
	if len(trackIDs) > 0 {
		if err := o.tuboClient.AddTracksToPlaylist(tuboPlaylist.ID, trackIDs); err != nil {
			return nil, fmt.Errorf("adding tracks to playlist: %w", err)
		}
	}
	o.writeToLog("✅ All tracks added successfully")

	return tuboPlaylist, nil
}

// reportResults prints a clean summary and detailed log
func (o *Orchestrator) reportResults(original *models.Playlist, created *models.Playlist, results []models.MatchResult) {
	successful := 0
	failed := 0
	var failedTracks []models.Track
	var lowScoreTracks []struct {
		Track models.Track
		Score float64
	}

	for _, result := range results {
		if result.Matched {
			successful++
			if result.MatchScore < 0.7 {
				lowScoreTracks = append(lowScoreTracks, struct {
					Track models.Track
					Score float64
				}{result.OriginalTrack, result.MatchScore})
			}
		} else {
			failed++
			failedTracks = append(failedTracks, result.OriginalTrack)
		}
	}

	// Clean terminal output
	fmt.Printf("\n🎉 PORTING RESULTS\n")
	fmt.Printf("==================\n")
	fmt.Printf("📋 Playlist: %s\n", original.Name)
	fmt.Printf("📊 Total tracks: %d\n", len(original.Tracks))
	fmt.Printf("✅ Successfully matched: %d\n", successful)
	fmt.Printf("❌ Failed to match: %d\n", failed)
	fmt.Printf("📈 Success rate: %.1f%%\n", float64(successful)/float64(len(original.Tracks))*100)

	if created != nil {
		fmt.Printf("🔗 YouTube playlist: https://www.youtube.com/playlist?list=%s\n", created.ID)
	}

	// Detailed results to log file
	o.writeToLog("\n=== FINAL RESULTS SUMMARY ===")
	o.writeToLog("Original playlist: %s", original.Name)
	o.writeToLog("Total tracks: %d", len(original.Tracks))
	o.writeToLog("Successfully matched: %d", successful)
	o.writeToLog("Failed to match: %d", failed)
	o.writeToLog("Success rate: %.1f%%", float64(successful)/float64(len(original.Tracks))*100)

	// Log failed tracks
	if len(failedTracks) > 0 {
		o.writeToLog("\n=== FAILED TO MATCH (%d tracks) ===", len(failedTracks))
		for i, track := range failedTracks {
			o.writeToLog("%d. \"%s\" by \"%s\"", i+1, track.Title, track.Artist)
		}
	}

	// Log low confidence matches
	if len(lowScoreTracks) > 0 {
		o.writeToLog("\n=== LOW CONFIDENCE MATCHES (%d tracks) ===", len(lowScoreTracks))
		for i, item := range lowScoreTracks {
			o.writeToLog("%d. \"%s\" by \"%s\" (score: %.2f)",
				i+1, item.Track.Title, item.Track.Artist, item.Score)
		}
	}

	// Show quick summary of failed tracks in terminal (first few)
	if len(failedTracks) > 0 {
		fmt.Printf("\n❌ Failed to match (%d tracks):\n", len(failedTracks))
		for i, track := range failedTracks {
			if i >= 5 {
				fmt.Printf("    ... and %d more (see log file for complete list)\n", len(failedTracks)-5)
				break
			}
			fmt.Printf("    • %s - %s\n", track.Artist, track.Title)
		}
	}

	// Analysis
	successRate := float64(successful) / float64(len(original.Tracks)) * 100
	fmt.Printf("\n📊 Analysis:\n")
	if successRate >= 80 {
		fmt.Printf("🎉 Excellent! Most tracks were successfully matched.\n")
	} else if successRate >= 60 {
		fmt.Printf("👍 Good results with room for improvement.\n")
	} else if successRate >= 40 {
		fmt.Printf("⚠️  Moderate success. Some tracks may not be available on YouTube.\n")
	} else {
		fmt.Printf("❌ Low success rate. Many tracks might be missing from YouTube.\n")
	}
}

// truncateString truncates a string to the specified length
func truncateString(s string, length int) string {
	if len(s) <= length {
		return s
	}
	if length <= 3 {
		return s[:length]
	}
	return s[:length-3] + "..."
}
