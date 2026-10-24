package acloserlisten

import (
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"golang.org/x/net/html"
)

// embedded is one release from the page, in document order. title is set for
// an album named in the prose; player releases get their title later.
type embedded struct {
	release release
	genre   string
	title   string
}

// embeddedReleases walks the content in document order, tracking the most
// recent genre heading and collecting unique Bandcamp players. An album URL
// wins over a track parameter in the same player, because that parameter only
// chooses the starting song.
func embeddedReleases(doc *html.Node) []embedded {
	var found []embedded
	seen := map[string]bool{}
	genre := ""
	var artist string
	titled := false
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.ElementNode {
			switch node.Data {
			case "strong":
				if label := genreLabel(node); label != "" {
					genre = label
					artist = ""
					titled = false
					return
				}
				if name := cleanName(proseName(node)); name != "" {
					artist = name
					titled = false
				}
				return
			case "em":
				if artist != "" && !titled {
					title := cleanName(textOf(node))
					if title != "" {
						titled = true
						if slug, ok := proseSlug(artist, title); ok {
							rel := release{kind: releaseNamed, id: slug}
							if !seen[rel.guid()] {
								seen[rel.guid()] = true
								found = append(found, embedded{release: rel, genre: genre, title: artist + " — " + title})
							}
						}
					}
				}
				return
			case "iframe":
				if rel, ok := embeddedRelease(attr(node, "src")); ok && !seen[rel.guid()] {
					seen[rel.guid()] = true
					found = append(found, embedded{release: rel, genre: genre})
				}
				return
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(doc)
	return found
}

// proseName is a bold run that does not introduce a genre section. Paired with
// the next italic title, it is the artist of a named album.
func proseName(strong *html.Node) string {
	if genreLabel(strong) != "" {
		return ""
	}
	var label strings.Builder
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.TextNode {
			label.WriteString(node.Data)
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(strong)
	return strings.TrimSpace(label.String())
}

// genreLabel reports the section a bold lead-in introduces. Sections are
// written as a bold genre followed by a line break, so a bold run without a
// line break is prose emphasis, not a genre.
func genreLabel(strong *html.Node) string {
	var label strings.Builder
	for child := strong.FirstChild; child != nil; child = child.NextSibling {
		if child.Type == html.ElementNode && child.Data == "br" {
			return strings.TrimSpace(label.String())
		}
		if child.Type == html.TextNode {
			label.WriteString(child.Data)
		}
	}
	return ""
}

// embeddedRelease validates a Bandcamp embedded player URL and returns the
// release it identifies. Album IDs and standalone track IDs are the identity
// of a recommendation, so only the documented player path on Bandcamp's own
// hosts is accepted.
func embeddedRelease(src string) (release, bool) {
	trimmed := strings.TrimSpace(src)
	if trimmed == "" {
		return release{}, false
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return release{}, false
	}
	if parsed.Scheme != "" && parsed.Scheme != "https" {
		return release{}, false
	}
	if id, ok := youtubeVideoID(parsed); ok {
		return release{kind: releaseYouTube, id: id}, true
	}
	if parsed.Host != playerHost && parsed.Host != playerAltHost {
		return release{}, false
	}
	if !strings.HasPrefix(parsed.Path, playerPathPrefix) {
		return release{}, false
	}
	var albumID, trackID string
	sawAlbum := false
	trackInvalid := false
	for _, segment := range strings.Split(parsed.Path, "/") {
		if value, ok := strings.CutPrefix(segment, "album="); ok {
			sawAlbum = true
			id, err := canonicalID(value)
			if err != nil {
				return release{}, false
			}
			if albumID == "" {
				albumID = id
			}
			continue
		}
		value, ok := strings.CutPrefix(segment, "track=")
		if !ok {
			continue
		}
		id, err := canonicalID(value)
		if err != nil {
			trackInvalid = true
			continue
		}
		if trackID == "" {
			trackID = id
		}
	}
	if sawAlbum {
		return release{kind: releaseAlbum, id: albumID}, true
	}
	if trackID == "" || trackInvalid {
		return release{}, false
	}
	return release{kind: releaseTrack, id: trackID}, true
}

// youtubeVideoID returns the video id from a /embed/<id> player URL.
func youtubeVideoID(parsed *url.URL) (string, bool) {
	switch parsed.Host {
	case "youtube.com", "www.youtube.com", "youtube-nocookie.com", "www.youtube-nocookie.com":
	default:
		return "", false
	}
	rest, ok := strings.CutPrefix(parsed.Path, "/embed/")
	if !ok || rest == "" {
		return "", false
	}
	if cut, _, found := strings.Cut(rest, "/"); found {
		rest = cut
	}
	if !validYouTubeID(rest) {
		return "", false
	}
	return rest, true
}

func proseSlug(artist, title string) (string, bool) {
	artistSlug, titleSlug := nameSlug(artist), nameSlug(title)
	slug := artistSlug + ":" + titleSlug
	if !validNameSlug(slug) {
		return "", false
	}
	return slug, true
}

// albumNames are the prose slugs a player's album title can be written as: the
// title itself, then with a trailing bracketed note or format word such as EP
// removed, one at a time. The lede names "Grand Reopening" for the player's
// "Grand Reopening EP".
func albumNames(artist, title string) []string {
	names := []string{}
	for title != "" {
		if slug, ok := proseSlug(artist, title); ok && !slices.Contains(names, slug) {
			names = append(names, slug)
		}
		shorter := strings.TrimRight(cleanName(trimEdition(title)), " -\u2013\u2014")
		if shorter == title {
			break
		}
		title = shorter
	}
	return names
}

// trimEdition removes one trailing bracketed note or release-format word.
func trimEdition(title string) string {
	title = strings.TrimSpace(title)
	for _, pair := range [][2]string{{"(", ")"}, {"[", "]"}} {
		if strings.HasSuffix(title, pair[1]) {
			if open := strings.LastIndex(title, pair[0]); open > 0 {
				return title[:open]
			}
		}
	}
	fields := strings.Fields(title)
	if len(fields) > 1 {
		switch strings.ToLower(fields[len(fields)-1]) {
		case "ep", "lp", "single", "e.p.":
			return strings.Join(fields[:len(fields)-1], " ")
		}
	}
	return title
}

func nameSlug(text string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(text) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func validNameSlug(slug string) bool {
	artist, title, ok := strings.Cut(slug, ":")
	if !ok || artist == "" || title == "" || strings.Contains(title, ":") {
		return false
	}
	return nameSlug(artist) == artist && nameSlug(title) == title
}

func cleanName(text string) string {
	return strings.Trim(strings.TrimSpace(text), " ,;:.")
}

func textOf(node *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			b.WriteString(n.Data)
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(node)
	return b.String()
}

func validYouTubeID(id string) bool {
	if len(id) != 11 {
		return false
	}
	for _, r := range id {
		switch {
		case r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_', r == '-':
		default:
			return false
		}
	}
	return true
}

func canonicalID(raw string) (string, error) {
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		return "", fmt.Errorf("not a positive id")
	}
	return strconv.FormatInt(id, 10), nil
}

func attr(node *html.Node, name string) string {
	for _, a := range node.Attr {
		if a.Key == name {
			return a.Val
		}
	}
	return ""
}

// description renders the genre a recommendation appeared under as plain text:
// the feed escapes it and no upstream markup is copied.
func description(genre string) string {
	if genre == "" {
		return ""
	}
	return "Genre: " + genre
}
