package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"playlistporter/internal/config"
	"playlistporter/internal/orchestrator"
)

func main() {
	var (
		sptURL     = flag.String("url", "", "SPT playlist URL to port")
		configPath = flag.String("config", "configs/config.yaml", "Path to configuration file")
		verbose    = flag.Bool("v", false, "Verbose output")
		logFile    = flag.String("log", "", "Log file path (optional). If empty, creates logs/porting_TIMESTAMP.log")
		maxTracks  = flag.Int("max-tracks", 50, "Maximum number of tracks to process in this session (default: 50)")
		showStates = flag.Bool("list-states", false, "List all saved porting states")
		syncMode   = flag.Bool("sync", false, "Check for new tracks on completed playlists and sync them")
	)
	flag.Parse()

	// If listing states, do that and exit
	if *showStates {
		listSavedStates()
		return
	}

	if *sptURL == "" {
		fmt.Println("Usage: playlistporter -url <spt-playlist-url>")
		fmt.Println("\nOptions:")
		flag.PrintDefaults()
		fmt.Println("\nExamples:")
		fmt.Println("  # Process first 50 tracks (default)")
		fmt.Println("  playlistporter -url https://open.spotify.com/playlist/...")
		fmt.Println("")
		fmt.Println("  # Process only 20 tracks (to save quota)")
		fmt.Println("  playlistporter -url https://open.spotify.com/playlist/... -max-tracks 20")
		fmt.Println("")
		fmt.Println("  # Check for new tracks on a completed playlist")
		fmt.Println("  playlistporter -url https://open.spotify.com/playlist/... -sync")
		fmt.Println("")
		fmt.Println("  # List all saved states")
		fmt.Println("  playlistporter -list-states")
		os.Exit(1)
	}

	// Validate max tracks
	if *maxTracks < 1 {
		log.Fatalf("max-tracks must be at least 1")
	}

	// Setup logging
	var logFilePath string
	if *logFile != "" {
		logFilePath = *logFile
	} else {
		// Create logs directory if it doesn't exist
		if err := os.MkdirAll("logs", 0755); err != nil {
			log.Fatalf("Failed to create logs directory: %v", err)
		}

		// Generate filename with timestamp
		timestamp := time.Now().Format("20060102_150405")
		logFilePath = fmt.Sprintf("logs/porting_%s.log", timestamp)
	}

	// Load configuration
	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	fmt.Printf("🎵 PlaylistPorter Starting\n")
	fmt.Printf("===========================\n")
	fmt.Printf("📋 Playlist URL: %s\n", *sptURL)
	fmt.Printf("🔢 Max tracks per session: %d\n", *maxTracks)
	if *syncMode {
		fmt.Printf("🔄 Sync mode: ENABLED (checking for new tracks)\n")
	}
	if *verbose {
		fmt.Printf("📝 Detailed logs: %s\n", logFilePath)
		fmt.Printf("💡 Follow progress: tail -f %s\n", logFilePath)
	}
	fmt.Printf("⏳ Processing...\n\n")

	// Show quota information
	showQuotaInfo(*maxTracks)

	// Initialize orchestrator with log file, max tracks, and sync mode
	orch := orchestrator.New(cfg, *verbose, logFilePath, *maxTracks, *syncMode)

	// Execute playlist porting
	if err := orch.PortPlaylist(*sptURL); err != nil {
		log.Fatalf("Failed to port playlist: %v", err)
	}

	if *verbose {
		fmt.Printf("\n📄 Full details saved to: %s\n", logFilePath)
	}
}

// showQuotaInfo displays information about YouTube API quota usage
func showQuotaInfo(maxTracks int) {
	fmt.Printf("\n📊 YouTube API Quota Information:\n")
	fmt.Printf("==================================\n")
	fmt.Printf("• Daily quota limit: 10,000 units\n")
	fmt.Printf("• Search cost: ~100 units per track\n")
	fmt.Printf("• Estimated usage: ~%d units for %d tracks\n", maxTracks*200, maxTracks)
	fmt.Printf("• Quota resets: Pacific Time midnight\n\n")

	if maxTracks > 50 {
		fmt.Printf("⚠️  Warning: Processing %d tracks may use significant quota!\n", maxTracks)
		fmt.Printf("   Consider using -max-tracks 50 or less.\n\n")
	}
}

// listSavedStates shows all saved porting states
func listSavedStates() {
	fmt.Printf("📂 Saved Porting States\n")
	fmt.Printf("======================\n\n")

	// List all files in states directory
	entries, err := os.ReadDir("states")
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Println("No saved states found. The 'states' directory doesn't exist yet.")
			fmt.Println("States will be created when you start porting a playlist.")
			return
		}
		log.Fatalf("Error reading states directory: %v", err)
	}

	if len(entries) == 0 {
		fmt.Println("No saved states found.")
		return
	}

	for _, entry := range entries {
		if !entry.IsDir() && entry.Name() != ".gitkeep" {
			info, err := entry.Info()
			if err != nil {
				continue
			}

			fmt.Printf("📄 %s\n", entry.Name())
			fmt.Printf("   Last modified: %s\n", info.ModTime().Format("2006-01-02 15:04:05"))
			fmt.Printf("   Size: %d bytes\n\n", info.Size())
		}
	}

	fmt.Println("💡 Tip: When you run the porter with the same playlist URL,")
	fmt.Println("   it will automatically resume from where it left off.")
}
