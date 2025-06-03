# PlaylistPorter 🎵

A Go-based tool to port playlists from SPT to TUBO Music, matching songs intelligently using metadata analysis.

## Features

- Extract complete playlist metadata from SPT
- Intelligent track matching using normalized metadata
- Advanced similarity scoring (titles, artists, ISRC, duration)
- Create playlists on TUBO Music with matched tracks
- Detailed matching reports and success rates
- Fast concurrent processing
- Secure OAuth2 authentication

## Architecture

```
CLI → Orchestrator → [SPT Client] → [Processor] → [TUBO Client] → Result
```

### Components

- **CLI**: Command-line interface and parameter handling
- **Orchestrator**: Coordinates the complete workflow
- **SPT Client**: Handles SPT API authentication and data fetching
- **TUBO Client**: Manages TUBO Music API interactions
- **Processor**: Normalizes metadata and calculates similarity scores
- **Models**: Shared data structures

## Prerequisites

1. **SPT Developer Account**
   - Create an app at https://developer.spt.com/
   - Get Client ID and Client Secret
   - Set redirect URI to `http://localhost:8080/callback`

2. **Google Cloud Console Setup**
   - Create a project at https://console.developers.google.com/
   - Enable TUBO Music API
   - Create OAuth2 credentials
   - Get Client ID and Client Secret

## Installation

1. **Clone the repository**
   ```bash
   git clone <repository-url>
   cd PlaylistPorter
   ```

2. **Install dependencies**
   ```bash
   go mod tidy
   ```

3. **Configure credentials**
   ```bash
   cp configs/config.example.yaml configs/config.yaml
   # Edit config.yaml with your API credentials
   ```

4. **Build the application**
   ```bash
   go build -o bin/playlistporter cmd/playlistporter/main.go
   ```

## Usage

### Basic Usage

```bash
./bin/playlistporter -url "https://open.spt.com/playlist/37i9dQZF1DXcBWIGoYBM5M"
```

### With Verbose Output

```bash
./bin/playlistporter -url "https://open.spt.com/playlist/37i9dQZF1DXcBWIGoYBM5M" -v
```

### Custom Config File

```bash
./bin/playlistporter -url "https://open.spt.com/playlist/37i9dQZF1DXcBWIGoYBM5M" -config "path/to/config.yaml"
```

## Configuration

Edit `configs/config.yaml`:

```yaml
spt:
  client_id: "your_spt_client_id"
  client_secret: "your_spt_client_secret"
  redirect_uri: "http://localhost:8080/callback"
  scopes:
    - "playlist-read-private"
    - "playlist-read-collaborative"

tubo:
  client_id: "your_tubo_client_id"
  client_secret: "your_tubo_client_secret"
  redirect_uri: "http://localhost:8080/callback"
  scopes:
    - "https://www.googleapis.com/auth/tubemusic"
```

## How It Works

### 1. Authentication
- Authenticates with both SPT and TUBO Music APIs using OAuth2
- Uses client credentials flow for SPT, OAuth2 for TUBO

### 2. Playlist Extraction
- Fetches complete playlist metadata from SPT
- Handles pagination for large playlists
- Extracts: title, artist, album, duration, ISRC, release year

### 3. Data Normalization
- Removes parenthetical content, remixes, features
- Normalizes text (lowercase, removes diacritics)
- Filters common words that don't help matching

### 4. Track Matching
- Searches TUBO Music for each track
- Uses advanced similarity scoring:
  - Title similarity (70% weight)
  - Artist similarity (30% weight)
  - ISRC exact match (bonus)
  - Duration proximity (bonus)

### 5. Playlist Creation
- Creates new playlist on TUBO Music
- Adds all successfully matched tracks
- Reports detailed results

## Matching Algorithm

The tool uses a sophisticated matching system:

1. **Text Normalization**: Removes noise from titles/artists
2. **Levenshtein Distance**: Calculates string similarity
3. **Weighted Scoring**: Prioritizes title over artist matching
4. **ISRC Matching**: Perfect matches for tracks with ISRC codes
5. **Duration Validation**: Confirms matches with similar durations

**Minimum Match Threshold**: 70% similarity required

## Example Output

```
Starting playlist porting from: https://open.spt.com/playlist/...
Fetching playlist from SPT...
Found playlist: My Awesome Playlist (47 tracks)
Processing track metadata...
Searching for tracks on TUBO Music...
Matching track 1/47: Artist Name - Song Title
Found match (score: 0.95): Artist Name - Song Title
Creating playlist on TUBO Music...

PORTING RESULTS
==================
Original playlist: My Awesome Playlist
Total tracks: 47
Successfully matched: 43
Failed to match: 4
Success rate: 91.5%
Created playlist ID: PLrAOx4F2LzHy...
```

## Project Structure

```
PlaylistPorter/
├── cmd/playlistporter/     # CLI entry point
├── internal/
│   ├── spt/               # SPT API client
│   ├── tubo/              # TUBO Music API client
│   ├── models/            # Data structures
│   ├── config/            # Configuration management
│   ├── processor/         # Data normalization and matching
│   └── orchestrator/      # Workflow coordination
├── configs/               # Configuration files
└── bin/                   # Compiled binaries
```

## Development

### Running Tests
```bash
go test ./...
```

### Building for Different Platforms
```bash
# Linux
GOOS=linux GOARCH=amd64 go build -o bin/playlistporter-linux cmd/playlistporter/main.go

# Windows
GOOS=windows GOARCH=amd64 go build -o bin/playlistporter.exe cmd/playlistporter/main.go

# macOS
GOOS=darwin GOARCH=amd64 go build -o bin/playlistporter-mac cmd/playlistporter/main.go
```

## Troubleshooting

### Common Issues

1. **Authentication Errors**
   - Verify API credentials in config.yaml
   - Check that redirect URIs match exactly
   - Ensure required scopes are enabled

2. **Low Match Rates**
   - Some tracks may not be available on TUBO Music
   - Regional restrictions can affect availability
   - Try increasing search results in TUBO client

3. **Rate Limiting**
   - Both APIs have rate limits
   - The tool includes basic rate limiting, but large playlists may need delays

### Debug Mode

Use `-v` flag for verbose output to see detailed matching process:

```bash
./bin/playlistporter -url "..." -v
```

## License

This project is for personal use. Make sure to comply with SPT and TUBO Music API terms of service.

## Contributing

1. Fork the repository
2. Create a feature branch
3. Make your changes
4. Add tests if applicable
5. Submit a pull request

## Security

- Never commit API credentials to version control
- Use environment variables for sensitive data in production
- Regularly rotate API keys
- Review API permissions and scopes