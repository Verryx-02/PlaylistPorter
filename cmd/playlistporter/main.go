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
	)
	flag.Parse()

	if *sptURL == "" {
		fmt.Println("Usage: playlistporter -url <spt-playlist-url>")
		flag.PrintDefaults()
		os.Exit(1)
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
	if *verbose {
		fmt.Printf("📝 Detailed logs: %s\n", logFilePath)
		fmt.Printf("💡 Follow progress: tail -f %s\n", logFilePath)
	}
	fmt.Printf("⏳ Processing...\n\n")

	// Initialize orchestrator with log file
	orch := orchestrator.New(cfg, *verbose, logFilePath)

	// Execute playlist porting
	if err := orch.PortPlaylist(*sptURL); err != nil {
		log.Fatalf("Failed to port playlist: %v", err)
	}

	fmt.Println("✅ Playlist ported successfully!")
	if *verbose {
		fmt.Printf("\n📄 Full details saved to: %s\n", logFilePath)
	}
}
