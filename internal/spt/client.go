package spt

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/clientcredentials"

	"playlistporter/internal/config"
	"playlistporter/internal/models"
)

const (
	baseURL = "https://api.spt.com/v1"
)

// Client represents a SPT API client
type Client struct {
	config     *config.SPTConfig
	httpClient *http.Client
	token      *oauth2.Token
}

// NewClient creates a new SPT client
func NewClient(cfg *config.SPTConfig) (*Client, error) {
	client := &Client{
		config: cfg,
	}

	if err := client.authenticate(); err != nil {
		return nil, fmt.Errorf("authentication failed: %w", err)
	}

	return client, nil
}

// authenticate performs OAuth2 client credentials flow
func (c *Client) authenticate() error {
	cfg := &clientcredentials.Config{
		ClientID:     c.config.ClientID,
		ClientSecret: c.config.ClientSecret,
		TokenURL:     "https://accounts.spt.com/api/token",
	}

	token, err := cfg.Token(context.Background())
	if err != nil {
		return fmt.Errorf("getting access token: %w", err)
	}

	c.token = token
	c.httpClient = cfg.Client(context.Background())

	return nil
}

// GetPlaylist fetches a playlist by ID
func (c *Client) GetPlaylist(playlistID string) (*models.Playlist, error) {
	url := fmt.Sprintf("%s/playlists/%s", baseURL, playlistID)

	playlist := &sptPlaylist{}
	if err := c.makeRequest("GET", url, nil, playlist); err != nil {
		return nil, err
	}

	// Fetch all tracks (SPT API paginates results)
	tracks, err := c.getAllPlaylistTracks(playlistID)
	if err != nil {
		return nil, fmt.Errorf("fetching playlist tracks: %w", err)
	}

	return &models.Playlist{
		ID:          playlist.ID,
		Name:        playlist.Name,
		Description: playlist.Description,
		Tracks:      tracks,
		TotalTracks: len(tracks),
		IsPublic:    playlist.Public,
		OwnerID:     playlist.Owner.ID,
	}, nil
}

// getAllPlaylistTracks fetches all tracks from a playlist (handles pagination)
func (c *Client) getAllPlaylistTracks(playlistID string) ([]models.Track, error) {
	var allTracks []models.Track
	url := fmt.Sprintf("%s/playlists/%s/tracks", baseURL, playlistID)

	for url != "" {
		response := &sptTracksResponse{}
		if err := c.makeRequest("GET", url, nil, response); err != nil {
			return nil, err
		}

		for _, item := range response.Items {
			if item.Track.ID != "" { // Skip local files or unavailable tracks
				track := models.Track{
					ID:          item.Track.ID,
					Title:       item.Track.Name,
					Artist:      getFirstArtist(item.Track.Artists),
					Album:       item.Track.Album.Name,
					Duration:    time.Duration(item.Track.DurationMS) * time.Millisecond,
					ReleaseYear: parseReleaseYear(item.Track.Album.ReleaseDate),
					ISRC:        getISRC(item.Track.ExternalIDs),
				}
				allTracks = append(allTracks, track)
			}
		}

		url = response.Next
	}

	return allTracks, nil
}

// makeRequest performs an HTTP request to SPT API
func (c *Client) makeRequest(method, url string, body interface{}, result interface{}) error {
	req, err := http.NewRequest(method, url, nil)
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

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("API request failed with status %d", resp.StatusCode)
	}

	if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
		return fmt.Errorf("decoding response: %w", err)
	}

	return nil
}

// Helper functions

func getFirstArtist(artists []sptArtist) string {
	if len(artists) > 0 {
		return artists[0].Name
	}
	return ""
}

func parseReleaseYear(releaseDate string) int {
	if len(releaseDate) >= 4 {
		var year int
		fmt.Sscanf(releaseDate[:4], "%d", &year)
		return year
	}
	return 0
}

func getISRC(externalIDs map[string]string) string {
	if isrc, ok := externalIDs["isrc"]; ok {
		return isrc
	}
	return ""
}

// SPT API response structures

type sptPlaylist struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Description string  `json:"description"`
	Public      bool    `json:"public"`
	Owner       sptUser `json:"owner"`
}

type sptUser struct {
	ID   string `json:"id"`
	Name string `json:"display_name"`
}

type sptTracksResponse struct {
	Items []sptTrackItem `json:"items"`
	Next  string         `json:"next"`
	Total int            `json:"total"`
}

type sptTrackItem struct {
	Track sptTrack `json:"track"`
}

type sptTrack struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Artists     []sptArtist       `json:"artists"`
	Album       sptAlbum          `json:"album"`
	DurationMS  int               `json:"duration_ms"`
	ExternalIDs map[string]string `json:"external_ids"`
}

type sptArtist struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type sptAlbum struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	ReleaseDate string `json:"release_date"`
}
