// Package arrivaudine watches Arriva Udine's service notices.
//
// The operator publishes its notices through the site's own WordPress notice
// endpoint, which the notice page is rendered from. The endpoint returns the
// most recent notices as JSON with a permalink and a publication date, so the
// permalink is the identity the feed compares and one request per poll is
// enough. Only the newest window is read: the endpoint also serves the whole
// archive, and an old notice is not news.
package arrivaudine

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"golang.org/x/net/html"

	"github.com/ppowo/rfs/internal/rfs"
	"github.com/ppowo/rfs/internal/sources/notices"
)

const (
	// PageURL is the notice endpoint the parser reads. It carries the window
	// and the fields the feed needs, so the response stays small.
	PageURL = "https://www.arrivaudine.it/wp-json/wp/v2/notice?per_page=10&orderby=date&order=desc&_fields=id,date,link,title,content"
	// HumanURL is the page subscribers should open for context.
	HumanURL = "https://www.arrivaudine.it/avvisi/"
)

type noticeEntry struct {
	Date  string `json:"date"`
	Link  string `json:"link"`
	Title struct {
		Rendered string `json:"rendered"`
	} `json:"title"`
	Content struct {
		Rendered string `json:"rendered"`
	} `json:"content"`
}

// ParseNotices decodes the notice window. A response without notices fails the
// poll instead of emptying the feed, because the endpoint always serves a
// notice history.
func ParseNotices(page rfs.Page) ([]notices.Notice, error) {
	var entries []noticeEntry
	if err := json.Unmarshal(page, &entries); err != nil {
		return nil, fmt.Errorf("arrivaudine: decode notices: %w", err)
	}
	if len(entries) == 0 {
		return nil, errors.New("arrivaudine: notice response carries no notices")
	}
	list := make([]notices.Notice, 0, len(entries))
	for _, entry := range entries {
		link := strings.TrimSpace(entry.Link)
		title := notices.NormalizeText(html.UnescapeString(entry.Title.Rendered))
		if link == "" || title == "" {
			return nil, errors.New("arrivaudine: notice without a permalink or a title")
		}
		list = append(list, notices.Notice{
			ID:      link,
			Title:   title,
			Summary: noticeBody(entry.Content.Rendered),
			Link:    link,
			Date:    noticeDate(entry.Date),
		})
	}
	return list, nil
}

// noticeBody renders the notice's own HTML as plain text. Notices that keep
// their text outside the endpoint carry no summary.
func noticeBody(rendered string) string {
	if strings.TrimSpace(rendered) == "" {
		return ""
	}
	doc, err := rfs.ParseHTML([]byte(rendered))
	if err != nil {
		return ""
	}
	return notices.TextOf(doc)
}

func noticeDate(stamp string) string {
	parsed, err := time.Parse("2006-01-02T15:04:05", strings.TrimSpace(stamp))
	if err != nil {
		return ""
	}
	return parsed.Format("02/01/2006")
}
