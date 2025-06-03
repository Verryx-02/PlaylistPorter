package tubo

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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

// authenticate performs OAuth2 authentication for YouTube using HTTP server
func (c *Client) authenticate() error {
	cfg := &oauth2.Config{
		ClientID:     c.config.ClientID,
		ClientSecret: c.config.ClientSecret,
		RedirectURL:  c.config.RedirectURI,
		Scopes:       c.config.Scopes,
		Endpoint:     google.Endpoint,
	}

	// Generate authorization URL
	authURL := cfg.AuthCodeURL("state",
		oauth2.AccessTypeOffline,
		oauth2.ApprovalForce)

	fmt.Println("\n🔐 YouTube Authentication Required")
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
	fmt.Println("\n💡 If the browser doesn't open automatically, copy the URL above and paste it in your browser")

	// Wait for either code or error
	var authCode string
	select {
	case authCode = <-codeChan:
		fmt.Println("✅ Authorization code received!")
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

	fmt.Println("✅ YouTube authentication successful!")
	return nil
}

// SearchTrack searches for a track and returns the best match
func (c *Client) SearchTrack(track models.Track) (*models.Track, float64, error) {
	// Build search query for music videos
	query := fmt.Sprintf("%s %s", track.Artist, track.Title)

	searchResults, err := c.search(query, "video")
	if err != nil {
		return nil, 0, err
	}

	if len(searchResults.Items) == 0 {
		return nil, 0, nil
	}

	// Find best match using similarity scoring
	bestMatch, score := c.findBestMatch(track, searchResults.Items)
	if score < 0.6 { // Lower threshold for YouTube videos
		return nil, 0, nil
	}

	return bestMatch, score, nil
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
	for _, trackID := range trackIDs {
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
	params.Set("q", query+" music") // Add "music" to improve results
	params.Set("type", searchType)
	params.Set("maxResults", "10")
	params.Set("videoCategoryId", "10") // Music category
	params.Set("order", "relevance")

	searchURL := baseURL + "/search?" + params.Encode()

	response := &youtubeSearchResponse{}
	if err := c.makeRequest("GET", searchURL, nil, response); err != nil {
		return nil, err
	}

	return response, nil
}

// findBestMatch uses similarity scoring to find the best matching track
func (c *Client) findBestMatch(original models.Track, candidates []youtubeSearchItem) (*models.Track, float64) {
	var bestMatch *models.Track
	var bestScore float64

	for _, candidate := range candidates {
		score := c.calculateSimilarity(original, candidate)
		if score > bestScore {
			bestScore = score
			bestMatch = &models.Track{
				ID:     candidate.ID.VideoID,
				Title:  candidate.Snippet.Title,
				Artist: candidate.Snippet.ChannelTitle,
			}
		}
	}

	return bestMatch, bestScore
}

// calculateSimilarity calculates similarity score between tracks
func (c *Client) calculateSimilarity(original models.Track, candidate youtubeSearchItem) float64 {
	// Clean up YouTube video title (remove common suffixes)
	cleanTitle := c.cleanVideoTitle(candidate.Snippet.Title)

	titleSim := stringSimilarity(
		strings.ToLower(original.Title),
		strings.ToLower(cleanTitle),
	)

	artistSim := stringSimilarity(
		strings.ToLower(original.Artist),
		strings.ToLower(candidate.Snippet.ChannelTitle),
	)

	// Check if the original artist appears in the video title
	if strings.Contains(strings.ToLower(candidate.Snippet.Title), strings.ToLower(original.Artist)) {
		artistSim += 0.2
	}

	// Weighted average: title is more important than artist for YouTube videos
	return (titleSim * 0.8) + (artistSim * 0.2)
}

// cleanVideoTitle removes common YouTube video suffixes and prefixes
func (c *Client) cleanVideoTitle(title string) string {
	// Common patterns to remove
	patterns := []string{
		"(Official Video)", "(Official Music Video)", "(Official Audio)",
		"(Lyric Video)", "(Lyrics)", "[Official Video]", "[Official Music Video]",
		"- Official Video", "- Official Music Video", "- Lyric Video",
		"(HD)", "[HD]", "(4K)", "[4K]", "- YouTube", "| YouTube",
	}

	cleaned := title
	for _, pattern := range patterns {
		cleaned = strings.ReplaceAll(cleaned, pattern, "")
	}

	return strings.TrimSpace(cleaned)
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

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("executing request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("API request failed with status %d", resp.StatusCode)
	}

	if result != nil {
		if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
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

// YouTube API structures

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
