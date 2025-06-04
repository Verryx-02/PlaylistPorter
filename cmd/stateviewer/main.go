package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"playlistporter/internal/state"
)

func main() {
	var (
		stateFile = flag.String("file", "", "State file to view")
		summary   = flag.Bool("summary", false, "Show summary of all states")
		detailed  = flag.Bool("detailed", false, "Show detailed information")
	)
	flag.Parse()

	if *summary || (*stateFile == "" && !*summary) {
		showAllStates()
		return
	}

	if *stateFile != "" {
		showStateDetails(*stateFile, *detailed)
	}
}

// showAllStates displays a summary of all saved states
func showAllStates() {
	fmt.Printf("📊 PlaylistPorter State Summary\n")
	fmt.Printf("================================\n\n")

	entries, err := os.ReadDir("states")
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Println("No states directory found.")
			return
		}
		fmt.Printf("Error reading states directory: %v\n", err)
		return
	}

	totalStates := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}

		stateFilePath := filepath.Join("states", entry.Name())
		data, err := os.ReadFile(stateFilePath)
		if err != nil {
			continue
		}

		var state state.PortingState
		if err := json.Unmarshal(data, &state); err != nil {
			continue
		}

		totalStates++
		fmt.Printf("📁 %s\n", state.OriginalPlaylist.Name)
		fmt.Printf("   Spotify ID: %s\n", state.SpotifyID)
		fmt.Printf("   Progress: %s", state.GetProgress())
		if state.IsComplete {
			fmt.Printf(" ✅ COMPLETE")
		}
		fmt.Printf("\n")
		fmt.Printf("   Sessions: %d\n", len(state.Sessions))
		fmt.Printf("   Last updated: %s\n", state.LastUpdatedAt.Format("2006-01-02 15:04"))
		if state.LastSyncCheck.Year() > 1 {
			fmt.Printf("   Last sync check: %s\n", state.LastSyncCheck.Format("2006-01-02 15:04"))
		}
		if state.YouTubePlaylistID != "" {
			fmt.Printf("   YouTube: https://www.youtube.com/playlist?list=%s\n", state.YouTubePlaylistID)
		}
		fmt.Printf("\n")
	}

	if totalStates == 0 {
		fmt.Println("No saved states found.")
	} else {
		fmt.Printf("Total playlists being ported: %d\n", totalStates)
	}
}

// showStateDetails shows detailed information about a specific state
func showStateDetails(filename string, detailed bool) {
	statePath := filename
	if !strings.Contains(statePath, string(os.PathSeparator)) {
		statePath = filepath.Join("states", filename)
	}

	data, err := os.ReadFile(statePath)
	if err != nil {
		fmt.Printf("Error reading state file: %v\n", err)
		return
	}
	if err != nil {
		fmt.Printf("Error reading state file: %v\n", err)
		return
	}

	var state state.PortingState
	if err := json.Unmarshal(data, &state); err != nil {
		fmt.Printf("Error parsing state file: %v\n", err)
		return
	}

	fmt.Printf("📋 Playlist: %s\n", state.OriginalPlaylist.Name)
	fmt.Printf("========================================\n\n")

	fmt.Printf("📊 Overall Progress\n")
	fmt.Printf("------------------\n")
	fmt.Printf("Spotify URL: %s\n", state.SpotifyURL)
	fmt.Printf("Progress: %s", state.GetProgress())
	if state.IsComplete {
		fmt.Printf(" ✅ COMPLETE")
	}
	fmt.Printf("\n")
	fmt.Printf("Created: %s\n", state.CreatedAt.Format("2006-01-02 15:04:05"))
	fmt.Printf("Last updated: %s\n", state.LastUpdatedAt.Format("2006-01-02 15:04:05"))

	if state.YouTubePlaylistID != "" {
		fmt.Printf("\n📺 YouTube Playlist\n")
		fmt.Printf("------------------\n")
		fmt.Printf("Name: %s\n", state.YouTubePlaylistName)
		fmt.Printf("URL: https://www.youtube.com/playlist?list=%s\n", state.YouTubePlaylistID)
	}

	// Session history
	fmt.Printf("\n📅 Session History (%d sessions)\n", len(state.Sessions))
	fmt.Printf("------------------\n")
	for i, session := range state.Sessions {
		duration := session.EndTime.Sub(session.StartTime)
		fmt.Printf("Session %d: %s\n", i+1, session.StartTime.Format("2006-01-02 15:04"))
		fmt.Printf("  Duration: %s\n", duration.Round(time.Second))
		fmt.Printf("  Tracks processed: %d\n", session.TracksProcessed)
		fmt.Printf("  Tracks matched: %d (%.1f%%)\n",
			session.TracksMatched,
			float64(session.TracksMatched)/float64(session.TracksProcessed)*100)
		fmt.Printf("  Est. quota used: ~%d units\n", session.QuotaUsed)
	}

	// Match statistics
	successful := 0
	failed := 0
	var failedTracks []string

	for _, result := range state.MatchResults {
		if result.Matched {
			successful++
		} else {
			failed++
			failedTracks = append(failedTracks,
				fmt.Sprintf("%s - %s", result.OriginalTrack.Artist, result.OriginalTrack.Title))
		}
	}

	fmt.Printf("\n📈 Match Statistics\n")
	fmt.Printf("------------------\n")
	fmt.Printf("Successful matches: %d\n", successful)
	fmt.Printf("Failed matches: %d\n", failed)
	if state.ProcessedTracks > 0 {
		fmt.Printf("Success rate: %.1f%%\n", float64(successful)/float64(state.ProcessedTracks)*100)
	}
	fmt.Printf("Total estimated quota used: ~%d units\n", state.GetTotalQuotaUsed())

	// Show failed tracks if requested
	if detailed && len(failedTracks) > 0 {
		fmt.Printf("\n❌ Failed Tracks (%d)\n", len(failedTracks))
		fmt.Printf("------------------\n")
		for i, track := range failedTracks {
			fmt.Printf("%d. %s\n", i+1, track)
			if i >= 20 && len(failedTracks) > 25 {
				fmt.Printf("... and %d more\n", len(failedTracks)-20)
				break
			}
		}
	}

	// Next steps
	if !state.IsComplete {
		remaining := state.TotalTracks - state.ProcessedTracks
		fmt.Printf("\n💡 Next Steps\n")
		fmt.Printf("------------------\n")
		fmt.Printf("Tracks remaining: %d\n", remaining)
		fmt.Printf("Sessions needed: ~%d (at 50 tracks/session)\n", (remaining+49)/50)
		fmt.Printf("Run the same command tomorrow to continue from track %d\n", state.ProcessedTracks+1)
	}
}
