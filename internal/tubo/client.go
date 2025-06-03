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

	"playlistporter/internal/config"
	"playlistporter/internal/models"
)

const (
	baseURL = "https://tubemusic.googleapis.com/tubemusic/v1"
)

// Client represents a TUBO Music API client
type Client struct {
	config     *config.TUBOConfig
	httpClient *http.Client
	token      *oauth2.Token
}

// NewClient creates a new TUBO Music client
func NewClient(cfg *config.TUBOConfig) (*Client, error) {
	client := &Client{
		config: cfg,
	}

	if err := client.authenticate(); err != nil {
		return nil, fmt.Errorf("authentication failed: %w", err)
	}

	return client, nil
}

// authenticate performs OAuth2 authentication for TUBO Music
func (c *Client) authenticate() error {
	cfg := &oauth2.Config{
		ClientID:     c.config.ClientID,
		ClientSecret: c.config.ClientSecret,
		RedirectURL:  c.config.RedirectURI,
		Scopes:       c.config.Scopes,
		Endpoint:     google.Endpoint,
	}

	// For now, this is a simplified auth flow
	// In a real implementation, you'd need to handle the OAuth2 flow properly
	token := &oauth2.Token{
		AccessToken: "placeholder_token",
		TokenType:   "Bearer",
		Expiry:      time.Now().Add(time.Hour),
	}

	c.token = token
	c.httpClient = cfg.Client(context.Background(), token)

	return nil
}

// SearchTrack searches for a track and returns the best match
func (c *Client) SearchTrack(track models.Track) (*models.Track, float64, error) {
	// Build search query
	query := fmt.Sprintf("%s %s", track.NormalizedArtist, track.NormalizedTitle)
	if query == " " {
		query = fmt.Sprintf("%s %s", track.Artist, track.Title)
	}

	searchResults, err := c.search(query, "song")
	if err != nil {
		return nil, 0, err
	}

	if len(searchResults.Items) == 0 {
		return nil, 0, nil
	}

	// Find best match using similarity scoring
	bestMatch, score := c.findBestMatch(track, searchResults.Items)
	if score < 0.7 { // Minimum threshold for considering a match
		return nil, 0, nil
	}

	return bestMatch, score, nil
}

// CreatePlaylist creates a new playlist on TUBO Music
func (c *Client) CreatePlaylist(name, description string) (*models.Playlist, error) {
	request := tuboCreatePlaylistRequest{
		Snippet: tuboPlaylistSnippet{
			Title:       name,
			Description: description,
		},
		Status: tuboPlaylistStatus{
			PrivacyStatus: "private",
		},
	}

	response := &tuboPlaylistResponse{}
	if err := c.makeRequest("POST", baseURL+"/playlists", request, response); err != nil {
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
		request := tuboPlaylistItemRequest{
			Snippet: tuboPlaylistItemSnippet{
				PlaylistID: playlistID,
				ResourceID: tuboResourceID{
					Kind:    "tubemusic#song",
					VideoID: trackID,
				},
			},
		}

		if err := c.makeRequest("POST", baseURL+"/playlistItems", request, nil); err != nil {
			return fmt.Errorf("adding track %s: %w", trackID, err)
		}
	}

	return nil
}

// search performs a search query on TUBO Music
func (c *Client) search(query, searchType string) (*tuboSearchResponse, error) {
	params := url.Values{}
	params.Set("part", "snippet")
	params.Set("q", query)
	params.Set("type", searchType)
	params.Set("maxResults", "10")

	searchURL := baseURL + "/search?" + params.Encode()

	response := &tuboSearchResponse{}
	if err := c.makeRequest("GET", searchURL, nil, response); err != nil {
		return nil, err
	}

	return response, nil
}

// findBestMatch uses similarity scoring to find the best matching track
func (c *Client) findBestMatch(original models.Track, candidates []tuboSearchItem) (*models.Track, float64) {
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
func (c *Client) calculateSimilarity(original models.Track, candidate tuboSearchItem) float64 {
	titleSim := stringSimilarity(
		strings.ToLower(original.Title),
		strings.ToLower(candidate.Snippet.Title),
	)

	artistSim := stringSimilarity(
		strings.ToLower(original.Artist),
		strings.ToLower(candidate.Snippet.ChannelTitle),
	)

	// Weighted average: title is more important than artist
	return (titleSim * 0.7) + (artistSim * 0.3)
}

// makeRequest performs an HTTP request to TUBO Music API
func (c *Client) makeRequest(method, url string, body interface{}, result interface{}) error {
	var reqBody []byte
	var err error

	if body != nil {
		reqBody, err = json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshaling request body: %w", err)
		}
	}

	req, err := http.NewRequest(method, url, bytes.NewBuffer(reqBody))
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}

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

// stringSimilarity calculates simple string similarity using longest common subsequence
func stringSimilarity(s1, s2 string) float64 {
	if s1 == s2 {
		return 1.0
	}

	// Simple implementation - could be improved with more sophisticated algorithms
	longer := s1
	shorter := s2
	if len(s2) > len(s1) {
		longer = s2
		shorter = s1
	}

	if len(longer) == 0 {
		return 1.0
	}

	return float64(len(shorter)) / float64(len(longer))
}

// TUBO Music API structures

type tuboSearchResponse struct {
	Items []tuboSearchItem `json:"items"`
}

type tuboSearchItem struct {
	ID      tuboVideoID `json:"id"`
	Snippet tuboSnippet `json:"snippet"`
}

type tuboVideoID struct {
	VideoID string `json:"videoId"`
}

type tuboSnippet struct {
	Title        string `json:"title"`
	ChannelTitle string `json:"channelTitle"`
	Description  string `json:"description"`
}

type tuboCreatePlaylistRequest struct {
	Snippet tuboPlaylistSnippet `json:"snippet"`
	Status  tuboPlaylistStatus  `json:"status"`
}

type tuboPlaylistSnippet struct {
	Title       string `json:"title"`
	Description string `json:"description"`
}

type tuboPlaylistStatus struct {
	PrivacyStatus string `json:"privacyStatus"`
}

type tuboPlaylistResponse struct {
	ID      string              `json:"id"`
	Snippet tuboPlaylistSnippet `json:"snippet"`
	Status  tuboPlaylistStatus  `json:"status"`
}

type tuboPlaylistItemRequest struct {
	Snippet tuboPlaylistItemSnippet `json:"snippet"`
}

type tuboPlaylistItemSnippet struct {
	PlaylistID string         `json:"playlistId"`
	ResourceID tuboResourceID `json:"resourceId"`
}

type tuboResourceID struct {
	Kind    string `json:"kind"`
	VideoID string `json:"videoId"`
}
