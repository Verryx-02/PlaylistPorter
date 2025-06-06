package tubo

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"

	"playlistporter/internal/auth"
	"playlistporter/internal/config"
	"playlistporter/internal/models"
)

const (
	baseURL = "https://www.googleapis.com/youtube/v3"
)

// Client represents a YouTube Data API client
type Client struct {
	config     *config.TUBOConfig
	httpClient *http.Client
	token      *oauth2.Token
	verbose    bool        // Add verbose logging
	logger     *log.Logger // Add file logger
}

// NewClient creates a new YouTube client
func NewClient(cfg *config.TUBOConfig) (*Client, error) {
	client := &Client{
		config: cfg,
	}

	if err := client.authenticate(); err != nil {
		return nil, fmt.Errorf("authentication failed: %w", err)
	}

	return client, nil
}

// SetVerbose enables detailed logging
func (c *Client) SetVerbose(verbose bool) {
	c.verbose = verbose
}

// SetLogger sets the file logger for detailed logging
func (c *Client) SetLogger(logger *log.Logger) {
	c.logger = logger
}

// logToFile writes to log file if logger is available
func (c *Client) logToFile(format string, args ...interface{}) {
	if c.logger != nil {
		c.logger.Printf(format, args...)
	}
}

// authenticate performs OAuth2 authentication for YouTube using HTTP server
func (c *Client) authenticate() error {
	cfg := &oauth2.Config{
		ClientID:     c.config.ClientID,
		ClientSecret: c.config.ClientSecret,
		RedirectURL:  c.config.RedirectURI,
		Scopes:       c.config.Scopes,
		Endpoint:     google.Endpoint,
	}

	// Debug: Print the scopes we're requesting
	fmt.Printf("Requesting OAuth scopes: %v\n", c.config.Scopes)

	// Generate authorization URL
	authURL := cfg.AuthCodeURL("state",
		oauth2.AccessTypeOffline,
		oauth2.ApprovalForce)

	fmt.Println("\nYouTube Authentication Required")
	fmt.Println("=====================================")
	fmt.Printf("1. Starting local HTTP server...\n")

	// Create channels for communication
	codeChan := make(chan string, 1)
	errChan := make(chan error, 1)

	// Start HTTP server in background
	go auth.StartHTTPServer("8080", codeChan, errChan)

	// Give server time to start
	time.Sleep(1 * time.Second)

	fmt.Printf("2. Opening authorization URL in browser:\n\n%s\n\n", authURL)
	fmt.Println("3. Complete the authorization in your browser")
	fmt.Println("4. The app will automatically receive the authorization code")
	fmt.Println("\nIf the browser doesn't open automatically, copy the URL above and paste it in your browser")
	fmt.Println("IMPORTANT: Make sure you see 'Manage your YouTube account' permissions in the browser!")

	// Wait for either code or error
	var authCode string
	select {
	case authCode = <-codeChan:
		fmt.Println("Authorization code received!")
	case err := <-errChan:
		return fmt.Errorf("HTTP server error: %w", err)
	case <-time.After(5 * time.Minute):
		return fmt.Errorf("authentication timeout - no response received within 5 minutes")
	}

	token, err := cfg.Exchange(context.Background(), authCode)
	if err != nil {
		return fmt.Errorf("exchanging authorization code: %w", err)
	}

	c.token = token
	c.httpClient = cfg.Client(context.Background(), token)

	// Debug: Print token info (without exposing the actual token)
	fmt.Printf("Token received. Expires: %v\n", token.Expiry)
	fmt.Printf("Token type: %s\n", token.TokenType)

	fmt.Println("YouTube authentication successful!")
	return nil
}

// SearchTrack searches for a track using optimized strategies (quota-friendly)
func (c *Client) SearchTrack(track models.Track) (*models.Track, float64, error) {
	// Reduced strategies to save quota - only the most effective ones
	searchStrategies := []string{
		fmt.Sprintf("%s %s", track.Artist, track.Title),         // Standard: "Artist Title"
		fmt.Sprintf("\"%s\" \"%s\"", track.Artist, track.Title), // Quoted: "Artist" "Title"
		// Removed other strategies to save quota
	}

	var bestMatch *models.Track
	var bestScore float64
	var bestStrategy string

	for i, query := range searchStrategies {
		c.logToFile("Strategy %d: \"%s\"", i+1, query)

		searchResults, err := c.search(query, "video")
		if err != nil {
			c.logToFile("Search error: %v", err)
			continue
		}

		if len(searchResults.Items) == 0 {
			c.logToFile("No results found")
			continue
		}

		// Find best match in this search
		match, score := c.findBestMatch(track, searchResults.Items)
		if match != nil {
			c.logToFile("Best result: \"%s\" by \"%s\" (score: %.2f)",
				c.cleanVideoTitle(match.Title), match.Artist, score)
		} else {
			c.logToFile("No decent matches in results")
		}

		// Update best overall match
		if score > bestScore {
			bestScore = score
			bestMatch = match
			bestStrategy = fmt.Sprintf("Strategy %d", i+1)
		}

		// If we found a good match, stop searching to save quota
		if score >= 0.75 { // Increased threshold to stop earlier
			c.logToFile("Good match found, stopping search to save quota")
			break
		}
	}

	// Lower minimum threshold but prioritize quota savings
	minThreshold := 0.5
	if bestScore < minThreshold {
		c.logToFile("Best score %.2f below threshold %.2f", bestScore, minThreshold)
		return nil, 0, nil
	}

	if bestMatch != nil {
		c.logToFile("Selected match using %s (score: %.2f)", bestStrategy, bestScore)
	}

	return bestMatch, bestScore, nil
}

// CreatePlaylist creates a new playlist on YouTube
func (c *Client) CreatePlaylist(name, description string) (*models.Playlist, error) {
	request := youtubeCreatePlaylistRequest{
		Snippet: youtubePlaylistSnippet{
			Title:       name,
			Description: description,
		},
		Status: youtubePlaylistStatus{
			PrivacyStatus: "private",
		},
	}

	// Debug: Print request details
	fmt.Printf("Creating playlist: \"%s\"\n", name)
	fmt.Printf("Request body: %+v\n", request)

	response := &youtubePlaylistResponse{}
	if err := c.makeRequest("POST", baseURL+"/playlists?part=snippet,status", request, response); err != nil {
		return nil, err
	}

	return &models.Playlist{
		ID:          response.ID,
		Name:        response.Snippet.Title,
		Description: response.Snippet.Description,
		IsPublic:    response.Status.PrivacyStatus == "public",
	}, nil
}

// AddTracksToPlaylist adds tracks to an existing playlist
func (c *Client) AddTracksToPlaylist(playlistID string, trackIDs []string) error {
	for i, trackID := range trackIDs {
		c.logToFile("Adding track %d/%d to playlist (Video ID: %s)", i+1, len(trackIDs), trackID)

		request := youtubePlaylistItemRequest{
			Snippet: youtubePlaylistItemSnippet{
				PlaylistID: playlistID,
				ResourceID: youtubeResourceID{
					Kind:    "youtube#video",
					VideoID: trackID,
				},
			},
		}

		if err := c.makeRequest("POST", baseURL+"/playlistItems?part=snippet", request, nil); err != nil {
			return fmt.Errorf("adding track %s: %w", trackID, err)
		}

		// Add a small delay to avoid rate limiting
		time.Sleep(100 * time.Millisecond)
	}

	return nil
}

// search performs a search query on YouTube
func (c *Client) search(query, searchType string) (*youtubeSearchResponse, error) {
	params := url.Values{}
	params.Set("part", "snippet")
	params.Set("q", query)
	params.Set("type", searchType)
	params.Set("maxResults", "15")      // Increased from 10 to 15
	params.Set("videoCategoryId", "10") // Music category
	params.Set("order", "relevance")

	searchURL := baseURL + "/search?" + params.Encode()

	response := &youtubeSearchResponse{}
	if err := c.makeRequest("GET", searchURL, nil, response); err != nil {
		return nil, err
	}

	return response, nil
}

// findBestMatch uses improved similarity scoring to find the best matching track
func (c *Client) findBestMatch(original models.Track, candidates []youtubeSearchItem) (*models.Track, float64) {
	var bestMatch *models.Track
	var bestScore float64

	for i, candidate := range candidates {
		score := c.calculateSimilarity(original, candidate)

		if i < 3 { // Show top 3 candidates in log
			cleanTitle := c.cleanVideoTitle(candidate.Snippet.Title)
			c.logToFile("   %d. \"%s\" by \"%s\" (score: %.2f)",
				i+1, cleanTitle, candidate.Snippet.ChannelTitle, score)
		}

		if score > bestScore {
			bestScore = score
			bestMatch = &models.Track{
				ID:     candidate.ID.VideoID,
				Title:  c.cleanVideoTitle(candidate.Snippet.Title),
				Artist: c.cleanChannelTitle(candidate.Snippet.ChannelTitle),
			}
		}
	}

	return bestMatch, bestScore
}

// calculateSimilarity calculates improved similarity score between tracks
func (c *Client) calculateSimilarity(original models.Track, candidate youtubeSearchItem) float64 {
	// Clean up YouTube video title and channel
	cleanTitle := c.cleanVideoTitle(candidate.Snippet.Title)
	cleanChannel := c.cleanChannelTitle(candidate.Snippet.ChannelTitle)

	// Calculate basic similarities
	titleSim := stringSimilarity(
		strings.ToLower(original.Title),
		strings.ToLower(cleanTitle),
	)

	artistSim := stringSimilarity(
		strings.ToLower(original.Artist),
		strings.ToLower(cleanChannel),
	)

	// Bonus points for various matching patterns
	var bonusScore float64

	// Check if original artist appears in video title
	if strings.Contains(strings.ToLower(candidate.Snippet.Title), strings.ToLower(original.Artist)) {
		bonusScore += 0.15
	}

	// Check if original title appears exactly in video title
	if strings.Contains(strings.ToLower(candidate.Snippet.Title), strings.ToLower(original.Title)) {
		bonusScore += 0.10
	}

	// Bonus for official channels/videos
	videoTitleLower := strings.ToLower(candidate.Snippet.Title)
	channelTitleLower := strings.ToLower(candidate.Snippet.ChannelTitle)

	if strings.Contains(videoTitleLower, "official") ||
		strings.Contains(channelTitleLower, "official") ||
		strings.Contains(channelTitleLower, "records") ||
		strings.HasSuffix(channelTitleLower, "vevo") {
		bonusScore += 0.05
	}

	// Penalty for covers, live versions, remixes (unless original also mentions them)
	originalTitleLower := strings.ToLower(original.Title)
	if (strings.Contains(videoTitleLower, "cover") && !strings.Contains(originalTitleLower, "cover")) ||
		(strings.Contains(videoTitleLower, "live") && !strings.Contains(originalTitleLower, "live")) ||
		(strings.Contains(videoTitleLower, "remix") && !strings.Contains(originalTitleLower, "remix")) {
		bonusScore -= 0.10
	}

	// Weighted average with bonuses
	finalScore := (titleSim * 0.7) + (artistSim * 0.3) + bonusScore

	// Cap at 1.0
	if finalScore > 1.0 {
		finalScore = 1.0
	}

	return finalScore
}

// cleanVideoTitle removes common YouTube video suffixes and prefixes
func (c *Client) cleanVideoTitle(title string) string {
	// More comprehensive list of patterns to remove
	patterns := []string{
		"(Official Video)", "(Official Music Video)", "(Official Audio)",
		"(Official Lyric Video)", "(Official HD Video)",
		"(Lyric Video)", "(Lyrics)", "(Audio)", "(Video)",
		"[Official Video]", "[Official Music Video]", "[Official Audio]",
		"[Lyric Video]", "[Lyrics]", "[Audio]", "[Video]",
		"- Official Video", "- Official Music Video", "- Official Audio",
		"- Lyric Video", "- Lyrics", "- Audio",
		"| Official Video", "| Official Music Video",
		"(HD)", "[HD]", "(4K)", "[4K]", "(1080p)", "[1080p]",
		"- YouTube", "| YouTube", "- Topic", "| Topic",
		"(Full Song)", "[Full Song]", "(Complete)", "[Complete]",
		"(Music Video)", "[Music Video]", "- Music Video",
		"(Original)", "[Original]", "- Original",
		"(HQ)", "[HQ]", "- HQ",
	}

	cleaned := title
	for _, pattern := range patterns {
		// Case insensitive replacement
		cleaned = replaceCaseInsensitive(cleaned, pattern, "")
	}

	// Remove extra whitespace and trim
	cleaned = strings.TrimSpace(cleaned)
	cleaned = strings.Join(strings.Fields(cleaned), " ")

	return cleaned
}

// cleanChannelTitle removes common channel suffixes
func (c *Client) cleanChannelTitle(channel string) string {
	// Remove common channel suffixes
	suffixes := []string{"VEVO", "Records", "Music", "Official", "- Topic"}

	cleaned := channel
	for _, suffix := range suffixes {
		cleaned = replaceCaseInsensitive(cleaned, suffix, "")
	}

	// Clean up spacing
	cleaned = strings.TrimSpace(cleaned)
	cleaned = strings.Join(strings.Fields(cleaned), " ")

	return cleaned
}

// replaceCaseInsensitive performs case-insensitive string replacement
func replaceCaseInsensitive(input, old, new string) string {
	oldLower := strings.ToLower(old)
	inputLower := strings.ToLower(input)

	for {
		index := strings.Index(inputLower, oldLower)
		if index == -1 {
			break
		}

		// Replace in original string maintaining case
		input = input[:index] + new + input[index+len(old):]
		inputLower = inputLower[:index] + new + inputLower[index+len(old):]
	}

	return input
}

// makeRequest performs an HTTP request to YouTube API
func (c *Client) makeRequest(method, requestURL string, body interface{}, result interface{}) error {
	var reqBody []byte
	var err error

	if body != nil {
		reqBody, err = json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshaling request body: %w", err)
		}
	}

	req, err := http.NewRequest(method, requestURL, bytes.NewBuffer(reqBody))
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}

	// Use OAuth for all requests
	req.Header.Set("Authorization", "Bearer "+c.token.AccessToken)
	req.Header.Set("Content-Type", "application/json")

	// Debug: Print request details (without exposing token)
	fmt.Printf("Making %s request to: %s\n", method, requestURL)
	if body != nil {
		fmt.Printf("Request body: %s\n", string(reqBody))
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("executing request: %w", err)
	}
	defer resp.Body.Close()

	// Read response body once for both debugging and processing
	var respBodyBytes []byte
	if resp.Body != nil {
		respBodyBytes, _ = io.ReadAll(resp.Body)
	}

	fmt.Printf("Response status: %d\n", resp.StatusCode)
	if len(respBodyBytes) > 0 {
		fmt.Printf("Response body: %s\n", string(respBodyBytes))
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("API request failed with status %d. URL: %s, Response: %s",
			resp.StatusCode, requestURL, string(respBodyBytes))
	}

	if result != nil && len(respBodyBytes) > 0 {
		if err := json.Unmarshal(respBodyBytes, result); err != nil {
			return fmt.Errorf("decoding response: %w", err)
		}
	}

	return nil
}

// Helper functions

// stringSimilarity calculates string similarity using Levenshtein distance
func stringSimilarity(s1, s2 string) float64 {
	if s1 == s2 {
		return 1.0
	}

	if len(s1) == 0 || len(s2) == 0 {
		return 0.0
	}

	// Calculate Levenshtein distance
	distance := levenshteinDistance(s1, s2)
	maxLen := len(s1)
	if len(s2) > maxLen {
		maxLen = len(s2)
	}

	return 1.0 - float64(distance)/float64(maxLen)
}

// levenshteinDistance calculates the Levenshtein distance between two strings
func levenshteinDistance(s1, s2 string) int {
	r1, r2 := []rune(s1), []rune(s2)
	len1, len2 := len(r1), len(r2)

	matrix := make([][]int, len1+1)
	for i := range matrix {
		matrix[i] = make([]int, len2+1)
	}

	for i := 0; i <= len1; i++ {
		matrix[i][0] = i
	}
	for j := 0; j <= len2; j++ {
		matrix[0][j] = j
	}

	for i := 1; i <= len1; i++ {
		for j := 1; j <= len2; j++ {
			cost := 1
			if r1[i-1] == r2[j-1] {
				cost = 0
			}

			matrix[i][j] = min(
				matrix[i-1][j]+1,
				matrix[i][j-1]+1,
				matrix[i-1][j-1]+cost,
			)
		}
	}

	return matrix[len1][len2]
}

func min(a, b, c int) int {
	if a < b && a < c {
		return a
	}
	if b < c {
		return b
	}
	return c
}

// YouTube API structures remain the same...

type youtubeSearchResponse struct {
	Items []youtubeSearchItem `json:"items"`
}

type youtubeSearchItem struct {
	ID      youtubeVideoID `json:"id"`
	Snippet youtubeSnippet `json:"snippet"`
}

type youtubeVideoID struct {
	VideoID string `json:"videoId"`
}

type youtubeSnippet struct {
	Title        string `json:"title"`
	ChannelTitle string `json:"channelTitle"`
	Description  string `json:"description"`
}

type youtubeCreatePlaylistRequest struct {
	Snippet youtubePlaylistSnippet `json:"snippet"`
	Status  youtubePlaylistStatus  `json:"status"`
}

type youtubePlaylistSnippet struct {
	Title       string `json:"title"`
	Description string `json:"description"`
}

type youtubePlaylistStatus struct {
	PrivacyStatus string `json:"privacyStatus"`
}

type youtubePlaylistResponse struct {
	ID      string                 `json:"id"`
	Snippet youtubePlaylistSnippet `json:"snippet"`
	Status  youtubePlaylistStatus  `json:"status"`
}

type youtubePlaylistItemRequest struct {
	Snippet youtubePlaylistItemSnippet `json:"snippet"`
}

type youtubePlaylistItemSnippet struct {
	PlaylistID string            `json:"playlistId"`
	ResourceID youtubeResourceID `json:"resourceId"`
}

type youtubeResourceID struct {
	Kind    string `json:"kind"`
	VideoID string `json:"videoId"`
}
