package processor

import (
	"regexp"
	"strings"
	"unicode"

	"playlistporter/internal/models"
)

// Processor handles data normalization and track matching logic
type Processor struct {
	// Common patterns to remove from track titles and artist names
	cleanupPatterns []*regexp.Regexp
}

// New creates a new Processor instance
func New() *Processor {
	patterns := []*regexp.Regexp{
		// Remove content in parentheses (often contains remix info, features, etc.)
		regexp.MustCompile(`\([^)]*\)`),
		// Remove content in square brackets
		regexp.MustCompile(`\[[^\]]*\]`),
		// Remove "feat." or "ft." and everything after
		regexp.MustCompile(`(?i)\s*f(ea)?t\..*`),
		// Remove "remix" and similar terms
		regexp.MustCompile(`(?i)\s*(remix|mix|edit|version|remaster).*`),
		// Remove extra whitespace
		regexp.MustCompile(`\s+`),
	}

	return &Processor{
		cleanupPatterns: patterns,
	}
}

// NormalizePlaylist normalizes all tracks in a playlist for better matching
func (p *Processor) NormalizePlaylist(playlist *models.Playlist) {
	for i := range playlist.Tracks {
		p.NormalizeTrack(&playlist.Tracks[i])
	}
}

// NormalizeTrack normalizes a single track's metadata
func (p *Processor) NormalizeTrack(track *models.Track) {
	track.NormalizedTitle = p.normalizeString(track.Title)
	track.NormalizedArtist = p.normalizeString(track.Artist)
}

// normalizeString applies various normalization techniques to improve matching
func (p *Processor) normalizeString(input string) string {
	if input == "" {
		return ""
	}

	// Convert to lowercase
	result := strings.ToLower(input)

	// Remove diacritics and special characters
	result = p.removeDiacritics(result)

	// Apply cleanup patterns
	for _, pattern := range p.cleanupPatterns {
		result = pattern.ReplaceAllString(result, " ")
	}

	// Remove common words that don't help with matching
	result = p.removeCommonWords(result)

	// Trim and normalize whitespace
	result = strings.TrimSpace(result)
	result = regexp.MustCompile(`\s+`).ReplaceAllString(result, " ")

	return result
}

// removeDiacritics removes accents and special characters
func (p *Processor) removeDiacritics(input string) string {
	// Simple implementation - maps common accented characters to their base forms
	replacements := map[rune]rune{
		'à': 'a', 'á': 'a', 'â': 'a', 'ã': 'a', 'ä': 'a', 'å': 'a',
		'è': 'e', 'é': 'e', 'ê': 'e', 'ë': 'e',
		'ì': 'i', 'í': 'i', 'î': 'i', 'ï': 'i',
		'ò': 'o', 'ó': 'o', 'ô': 'o', 'õ': 'o', 'ö': 'o',
		'ù': 'u', 'ú': 'u', 'û': 'u', 'ü': 'u',
		'ñ': 'n', 'ç': 'c',
		'ý': 'y', 'ÿ': 'y',
	}

	var result strings.Builder
	for _, r := range input {
		if replacement, exists := replacements[unicode.ToLower(r)]; exists {
			result.WriteRune(replacement)
		} else if unicode.IsPrint(r) && (unicode.IsLetter(r) || unicode.IsNumber(r) || unicode.IsSpace(r)) {
			result.WriteRune(r)
		} else {
			result.WriteRune(' ')
		}
	}

	return result.String()
}

// removeCommonWords removes words that don't contribute to matching accuracy
func (p *Processor) removeCommonWords(input string) string {
	commonWords := map[string]bool{
		"the": true, "a": true, "an": true, "and": true, "or": true,
		"but": true, "in": true, "on": true, "at": true, "to": true,
		"for": true, "of": true, "with": true, "by": true, "is": true,
		"are": true, "was": true, "were": true, "be": true, "been": true,
		"have": true, "has": true, "had": true, "do": true, "does": true,
		"did": true, "will": true, "would": true, "could": true, "should": true,
		"may": true, "might": true, "must": true, "can": true, "shall": true,
		"official": true, "music": true, "video": true, "audio": true,
		"lyric": true, "lyrics": true, "hd": true, "hq": true,
	}

	words := strings.Fields(input)
	var filtered []string

	for _, word := range words {
		word = strings.TrimSpace(word)
		if word != "" && !commonWords[word] {
			filtered = append(filtered, word)
		}
	}

	return strings.Join(filtered, " ")
}

// CalculateMatchScore calculates a similarity score between two tracks
func (p *Processor) CalculateMatchScore(track1, track2 models.Track) float64 {
	titleScore := p.stringSimilarity(track1.NormalizedTitle, track2.NormalizedTitle)
	artistScore := p.stringSimilarity(track1.NormalizedArtist, track2.NormalizedArtist)

	// Weight title more heavily than artist
	finalScore := (titleScore * 0.7) + (artistScore * 0.3)

	// Bonus points for exact ISRC match
	if track1.ISRC != "" && track1.ISRC == track2.ISRC {
		finalScore = 1.0
	}

	// Bonus for similar duration (within 10 seconds)
	if track1.Duration > 0 && track2.Duration > 0 {
		durationDiff := track1.Duration - track2.Duration
		if durationDiff < 0 {
			durationDiff = -durationDiff
		}
		if durationDiff <= 10*1e9 { // 10 seconds in nanoseconds
			finalScore += 0.1
		}
	}

	// Cap at 1.0
	if finalScore > 1.0 {
		finalScore = 1.0
	}

	return finalScore
}

// stringSimilarity calculates similarity between two strings using Levenshtein distance
func (p *Processor) stringSimilarity(s1, s2 string) float64 {
	if s1 == s2 {
		return 1.0
	}

	if len(s1) == 0 || len(s2) == 0 {
		return 0.0
	}

	// Use Levenshtein distance for better similarity calculation
	distance := p.levenshteinDistance(s1, s2)
	maxLen := len(s1)
	if len(s2) > maxLen {
		maxLen = len(s2)
	}

	return 1.0 - float64(distance)/float64(maxLen)
}

// levenshteinDistance calculates the Levenshtein distance between two strings
func (p *Processor) levenshteinDistance(s1, s2 string) int {
	r1, r2 := []rune(s1), []rune(s2)
	len1, len2 := len(r1), len(r2)

	// Create a matrix to store distances
	matrix := make([][]int, len1+1)
	for i := range matrix {
		matrix[i] = make([]int, len2+1)
	}

	// Initialize first row and column
	for i := 0; i <= len1; i++ {
		matrix[i][0] = i
	}
	for j := 0; j <= len2; j++ {
		matrix[0][j] = j
	}

	// Fill the matrix
	for i := 1; i <= len1; i++ {
		for j := 1; j <= len2; j++ {
			cost := 1
			if r1[i-1] == r2[j-1] {
				cost = 0
			}

			matrix[i][j] = min(
				matrix[i-1][j]+1,      // deletion
				matrix[i][j-1]+1,      // insertion
				matrix[i-1][j-1]+cost, // substitution
			)
		}
	}

	return matrix[len1][len2]
}

// min returns the minimum of three integers
func min(a, b, c int) int {
	if a < b && a < c {
		return a
	}
	if b < c {
		return b
	}
	return c
}
