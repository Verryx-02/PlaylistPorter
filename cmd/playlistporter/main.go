package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"playlistporter/internal/config"
	"playlistporter/internal/orchestrator"
)

func main() {
	var (
		sptURL     = flag.String("url", "", "SPT playlist URL to port")
		configPath = flag.String("config", "configs/config.yaml", "Path to configuration file")
		verbose    = flag.Bool("v", false, "Verbose output")
	)
	flag.Parse()

	if *sptURL == "" {
		fmt.Println("Usage: playlistporter -url <spt-playlist-url>")
		flag.PrintDefaults()
		os.Exit(1)
	}

	// Load configuration
	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Initialize orchestrator
	orch := orchestrator.New(cfg, *verbose)

	// Execute playlist porting
	if err := orch.PortPlaylist(*sptURL); err != nil {
		log.Fatalf("Failed to port playlist: %v", err)
	}

	fmt.Println("✅ Playlist ported successfully!")
}
