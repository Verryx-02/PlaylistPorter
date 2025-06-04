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
	"playlistporter/internal/state"
	"playlistporter/internal/tubo"
)

// Orchestrator coordinates the playlist porting workflow
type Orchestrator struct {
	cfg       *config.Config
	verbose   bool
	logFile   *os.File
	logger    *log.Logger
	maxTracks int  // Maximum tracks to process in this session
	syncMode  bool // Whether to check for new tracks on completed playlists

	sptClient    *spt.Client
	tuboClient   *tubo.Client
	processor    *processor.Processor
	stateManager *state.Manager
}

// New creates a new Orchestrator instance with optional log file
func New(cfg *config.Config, verbose bool, logFilePath string, maxTracks int, syncMode bool) *Orchestrator {
	orch := &Orchestrator{
		cfg:       cfg,
		verbose:   verbose,
		maxTracks: maxTracks,
		syncMode:  syncMode,
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
			orch.writeToLog("Max tracks per session: %d", maxTracks)
			if syncMode {
				orch.writeToLog("Sync mode: ENABLED")
			}
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

// PortPlaylist executes the complete playlist porting workflow with checkpoint support
func (o *Orchestrator) PortPlaylist(sptURL string) error {
	defer o.Close() // Ensure log file is closed when done

	o.writeToLog("Starting playlist porting from: %s", sptURL)

	// Step 1: Initialize clients and state manager
	if err := o.initializeClients(); err != nil {
		return fmt.Errorf("initializing clients: %w", err)
	}

	// Step 2: Extract playlist ID from URL
	playlistID, err := o.extractPlaylistID(sptURL)
	if err != nil {
		return fmt.Errorf("extracting playlist ID: %w", err)
	}
	o.writeToLog("Extracted playlist ID: %s", playlistID)

	// Step 3: Load or create state
	portingState, isNewState, err := o.loadOrCreateState(sptURL, playlistID)
	if err != nil {
		return fmt.Errorf("loading state: %w", err)
	}

	// Step 4: If resuming, show progress
	if !isNewState {
		fmt.Printf("📂 Resuming previous porting session\n")
		fmt.Printf("   Progress: %s\n", portingState.GetProgress())
		fmt.Printf("   Sessions completed: %d\n", len(portingState.Sessions))
		if portingState.YouTubePlaylistID != "" {
			fmt.Printf("   YouTube playlist: https://www.youtube.com/playlist?list=%s\n", portingState.YouTubePlaylistID)
		}
		o.writeToLog("Resuming from checkpoint: %s", portingState.GetProgress())
	}

	// Check if already complete
	if portingState.IsComplete && !o.syncMode {
		fmt.Printf("✅ This playlist has already been completely processed!\n")
		o.reportFinalResults(portingState)
		return nil
	}

	// If in sync mode and playlist is complete, check for new tracks
	if portingState.IsComplete && o.syncMode {
		fmt.Printf("🔄 Sync mode enabled - checking for new tracks...\n")

		// Fetch current playlist from Spotify
		currentPlaylist, err := o.sptClient.GetPlaylist(playlistID)
		if err != nil {
			return fmt.Errorf("fetching current playlist: %w", err)
		}

		// Detect new tracks
		newTracks := portingState.DetectNewTracks(*currentPlaylist)

		if len(newTracks) == 0 {
			fmt.Printf("✅ Playlist is up to date! No new tracks found.\n")
			fmt.Printf("   Last sync: %s\n", portingState.LastSyncCheck.Format("2006-01-02 15:04"))
			return nil
		}

		fmt.Printf("🆕 Found %d new tracks added to the Spotify playlist!\n", len(newTracks))

		// Update state for sync
		portingState.UpdateForSync(*currentPlaylist)

		// Update the playlist tracks to include new ones
		portingState.OriginalPlaylist.Tracks = currentPlaylist.Tracks

		// Log new tracks
		o.writeToLog("\n=== NEW TRACKS DETECTED ===")
		for i, track := range newTracks {
			o.writeToLog("%d. \"%s\" by \"%s\"", i+1, track.Title, track.Artist)
		}
	}

	// Step 5: Get next batch of tracks to process
	tracksToProcess := portingState.GetNextBatch(o.maxTracks)
	if len(tracksToProcess) == 0 {
		fmt.Printf("✅ No more tracks to process!\n")
		return nil
	}

	fmt.Printf("\n📋 Processing batch: %d tracks (starting from track %d)\n",
		len(tracksToProcess), portingState.ProcessedTracks+1)

	// Start new session tracking
	portingState.StartNewSession()

	// Step 6: Process and normalize track data
	fmt.Printf("🔧 Processing track metadata...\n")
	o.writeToLog("\n=== NORMALIZING METADATA (Batch) ===")

	// Create a temporary playlist with just the tracks to process
	batchPlaylist := &models.Playlist{
		Tracks: tracksToProcess,
	}
	o.processor.NormalizePlaylist(batchPlaylist)

	// Step 7: Search and match tracks on YouTube
	fmt.Printf("🔍 Searching for tracks on YouTube...\n")
	if o.verbose {
		fmt.Printf("    💡 Detailed search progress is being logged to file\n")
	}

	o.writeToLog("\n=== YOUTUBE SEARCH & MATCHING (Batch) ===")
	matchResults, err := o.matchTracks(batchPlaylist.Tracks, portingState.ProcessedTracks)
	if err != nil {
		return fmt.Errorf("matching tracks: %w", err)
	}

	// Step 8: Update state with results
	portingState.AddMatchResults(matchResults)

	// Count successful matches in this session
	sessionMatches := 0
	for _, result := range matchResults {
		if result.Matched {
			sessionMatches++
		}
	}
	portingState.EndCurrentSession(len(matchResults), sessionMatches)

	// Step 9: Create or update YouTube playlist
	if err := o.manageYouTubePlaylist(portingState, matchResults); err != nil {
		return fmt.Errorf("managing YouTube playlist: %w", err)
	}

	// Step 10: Save state
	if err := o.stateManager.SaveState(portingState); err != nil {
		return fmt.Errorf("saving state: %w", err)
	}
	fmt.Printf("💾 Progress saved to checkpoint\n")

	// Step 11: Report session results
	o.reportSessionResults(portingState, matchResults)

	// Check if we're done
	if portingState.IsComplete {
		fmt.Printf("\n🎉 Playlist porting completed!\n")
		o.reportFinalResults(portingState)

		if !o.syncMode {
			fmt.Printf("\n💡 Tip: Run with -sync flag to check for new tracks added to the Spotify playlist\n")
		}
	} else {
		remainingTracks := portingState.TotalTracks - portingState.ProcessedTracks
		fmt.Printf("\n⏸️  Session complete. %d tracks remaining.\n", remainingTracks)
		fmt.Printf("📅 Run again tomorrow to continue (YouTube quota resets daily)\n")
		fmt.Printf("💡 Next run will automatically resume from track %d\n", portingState.ProcessedTracks+1)
	}

	return nil
}

// loadOrCreateState loads existing state or creates new one
func (o *Orchestrator) loadOrCreateState(sptURL, playlistID string) (*state.PortingState, bool, error) {
	// Load existing state if it exists
	existingState, err := o.stateManager.LoadState(playlistID)
	if err != nil {
		return nil, false, fmt.Errorf("loading state: %w", err)
	}

	if existingState != nil {
		o.writeToLog("Loaded existing state for playlist %s", playlistID)

		// Migrate old state files if needed
		if existingState.NeedsMigration() {
			fmt.Printf("📦 Migrating state file to support new features...\n")
			existingState.Migrate()
			// Save migrated state
			if err := o.stateManager.SaveState(existingState); err != nil {
				return nil, false, fmt.Errorf("saving migrated state: %w", err)
			}
		}

		return existingState, false, nil
	}

	// No existing state, fetch playlist and create new state
	fmt.Printf("🎵 Fetching playlist from Spotify...\n")
	o.writeToLog("Fetching playlist from SPT...")

	playlist, err := o.sptClient.GetPlaylist(playlistID)
	if err != nil {
		return nil, false, fmt.Errorf("fetching SPT playlist: %w", err)
	}

	fmt.Printf("📋 Found playlist: \"%s\" (%d tracks)\n", playlist.Name, len(playlist.Tracks))
	o.writeToLog("Found playlist: %s (%d tracks)", playlist.Name, len(playlist.Tracks))

	// Create new state
	newState := o.stateManager.CreateNewState(sptURL, playlistID, *playlist)
	o.writeToLog("Created new state for playlist")

	// Save initial state
	if err := o.stateManager.SaveState(newState); err != nil {
		return nil, false, fmt.Errorf("saving initial state: %w", err)
	}

	return newState, true, nil
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

	// Initialize state manager
	stateManager, err := state.NewManager("states")
	if err != nil {
		return fmt.Errorf("creating state manager: %w", err)
	}
	o.stateManager = stateManager
	o.writeToLog("✅ State manager initialized")

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

// matchTracks searches for each track on YouTube (with offset for progress display)
func (o *Orchestrator) matchTracks(tracks []models.Track, startOffset int) ([]models.MatchResult, error) {
	results := make([]models.MatchResult, 0, len(tracks))

	for i, track := range tracks {
		actualTrackNumber := startOffset + i + 1

		// Show progress in terminal (clean)
		fmt.Printf("\r🎵 Matching tracks: %d/%d - %s",
			actualTrackNumber,
			startOffset+len(tracks),
			truncateString(fmt.Sprintf("%s - %s", track.Artist, track.Title), 40))

		// Detailed logging to file
		o.writeToLog("\n--- TRACK %d ---", actualTrackNumber)
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
	fmt.Printf("\r🎵 Batch matching complete!                                        \n")

	return results, nil
}

// manageYouTubePlaylist creates or updates the YouTube playlist
func (o *Orchestrator) manageYouTubePlaylist(portingState *state.PortingState, newResults []models.MatchResult) error {
	// Get video IDs from new results
	var newVideoIDs []string
	for _, result := range newResults {
		if result.Matched && result.MatchedTrack != nil {
			newVideoIDs = append(newVideoIDs, result.MatchedTrack.ID)
		}
	}

	if len(newVideoIDs) == 0 {
		o.writeToLog("No new tracks to add to YouTube playlist")
		return nil
	}

	// If playlist doesn't exist yet, create it
	if portingState.YouTubePlaylistID == "" {
		playlistName := fmt.Sprintf("%s (Ported from Spotify)", portingState.OriginalPlaylist.Name)
		description := fmt.Sprintf("Ported from Spotify using PlaylistPorter. Original: %s", portingState.SpotifyURL)

		fmt.Printf("📝 Creating YouTube playlist: \"%s\"\n", playlistName)
		o.writeToLog("Creating YouTube playlist: %s", playlistName)

		playlist, err := o.tuboClient.CreatePlaylist(playlistName, description)
		if err != nil {
			return fmt.Errorf("creating playlist: %w", err)
		}

		portingState.YouTubePlaylistID = playlist.ID
		portingState.YouTubePlaylistName = playlist.Name
		o.writeToLog("Created playlist with ID: %s", playlist.ID)
	}

	// Add new tracks to playlist
	fmt.Printf("📝 Adding %d tracks to YouTube playlist...\n", len(newVideoIDs))
	o.writeToLog("Adding %d tracks to existing playlist %s", len(newVideoIDs), portingState.YouTubePlaylistID)

	if err := o.tuboClient.AddTracksToPlaylist(portingState.YouTubePlaylistID, newVideoIDs); err != nil {
		return fmt.Errorf("adding tracks to playlist: %w", err)
	}

	o.writeToLog("✅ Tracks added successfully")
	return nil
}

// reportSessionResults prints results for the current session
func (o *Orchestrator) reportSessionResults(portingState *state.PortingState, sessionResults []models.MatchResult) {
	successful := 0
	failed := 0

	for _, result := range sessionResults {
		if result.Matched {
			successful++
		} else {
			failed++
		}
	}

	fmt.Printf("\n📊 SESSION RESULTS\n")
	fmt.Printf("==================\n")
	fmt.Printf("🎵 Tracks processed: %d\n", len(sessionResults))
	fmt.Printf("✅ Successfully matched: %d\n", successful)
	fmt.Printf("❌ Failed to match: %d\n", failed)
	fmt.Printf("📈 Session success rate: %.1f%%\n", float64(successful)/float64(len(sessionResults))*100)
	fmt.Printf("\n📊 OVERALL PROGRESS\n")
	fmt.Printf("==================\n")
	fmt.Printf("📋 Total progress: %s\n", portingState.GetProgress())
	fmt.Printf("🔗 YouTube playlist: https://www.youtube.com/playlist?list=%s\n", portingState.YouTubePlaylistID)

	// Estimate quota usage
	quotaEstimate := len(sessionResults) * 200 // Rough estimate
	fmt.Printf("📊 Estimated quota used this session: ~%d units\n", quotaEstimate)
	fmt.Printf("📊 Total estimated quota used: ~%d units\n", portingState.GetTotalQuotaUsed())
}

// reportFinalResults prints final summary when porting is complete
func (o *Orchestrator) reportFinalResults(portingState *state.PortingState) {
	successful := 0
	failed := 0
	var failedTracks []models.Track

	for _, result := range portingState.MatchResults {
		if result.Matched {
			successful++
		} else {
			failed++
			failedTracks = append(failedTracks, result.OriginalTrack)
		}
	}

	fmt.Printf("\n🎉 FINAL RESULTS\n")
	fmt.Printf("==================\n")
	fmt.Printf("📋 Playlist: %s\n", portingState.OriginalPlaylist.Name)
	fmt.Printf("📊 Total tracks: %d\n", portingState.TotalTracks)
	fmt.Printf("✅ Successfully matched: %d\n", successful)
	fmt.Printf("❌ Failed to match: %d\n", failed)
	fmt.Printf("📈 Success rate: %.1f%%\n", float64(successful)/float64(portingState.TotalTracks)*100)
	fmt.Printf("📅 Sessions required: %d\n", len(portingState.Sessions))
	fmt.Printf("🔗 YouTube playlist: https://www.youtube.com/playlist?list=%s\n", portingState.YouTubePlaylistID)

	// Show failed tracks
	if len(failedTracks) > 0 && len(failedTracks) <= 10 {
		fmt.Printf("\n❌ Failed to match:\n")
		for _, track := range failedTracks {
			fmt.Printf("    • %s - %s\n", track.Artist, track.Title)
		}
	} else if len(failedTracks) > 10 {
		fmt.Printf("\n❌ Failed to match %d tracks (showing first 10):\n", len(failedTracks))
		for i := 0; i < 10; i++ {
			fmt.Printf("    • %s - %s\n", failedTracks[i].Artist, failedTracks[i].Title)
		}
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
