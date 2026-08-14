package ptvjobs

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/ppowo/rfs/internal/rfs"
)

const (
	baseURL = "https://ptv-logistics.jobs.personio.com"

	// PageURL is the Personio listing of PTV Logistics open postings, and the
	// human-facing link for the source.
	PageURL = baseURL + "/?language=en"

	guidPrefix = "ptv-jobs:"

	// jobsKey locates the postings array inside the page's RSC payload.
	jobsKey = `"jobs":`

	// italyTerm matches a posting office that is remote-eligible for Italy.
	// PTV labels such postings "Remote - Italy" (and sometimes "remote - italy").
	// Only that label qualifies: offices like Perugia or Monza are intentionally
	// excluded because rfs tracks Italy-remote postings only.
	italyTerm = "italy"
)

// ExtractVersion is the derivation version for the PTV jobs Flow. Bump it
// whenever Extract's output can change for a fixed page so rfs forces a full
// re-derivation instead of trusting a stale HTTP 304.
const ExtractVersion = 1

type Flow struct{}

// Version reports the PTV jobs extraction version.
func (Flow) Version() int { return ExtractVersion }

// personioJob is a posting record embedded in the listing page's RSC payload.
type personioJob struct {
	ID                string   `json:"id"`
	Name              string   `json:"name"`
	MainOffice        string   `json:"main_office"`
	AdditionalOffices []string `json:"additional_offices"`
	Schedule          string   `json:"schedule"`
	Department        string   `json:"department"`
}

// nextFPush matches a complete RSC payload chunk: self.__next_f.push([...])
// up to the closing </script>. Chunk contents are JSON arrays whose string
// elements concatenate to the flight payload.
var nextFPush = regexp.MustCompile(`(?s)self\.__next_f\.push\((\[.*?\])\);?</script>`)

// Extract decodes the postings array from the Personio listing's RSC payload
// and emits one item per posting whose main or additional office is marked
// "Remote - Italy". Listing membership alone defines the current Items: a
// posting that leaves the listing naturally leaves the feed. A page that
// yields no decodable postings is an error, never an empty feed, so a Personio
// markup change surfaces as a poll failure instead of mass item deletion.
func (Flow) Extract(page rfs.Page) ([]rfs.ExtractedItem, error) {
	payload, err := extractJobsPayload(page)
	if err != nil {
		return nil, fmt.Errorf("ptvjobs: %w", err)
	}

	var jobs []personioJob
	if err := json.Unmarshal(payload, &jobs); err != nil {
		return nil, fmt.Errorf("ptvjobs: decode jobs array: %w", err)
	}
	if len(jobs) == 0 {
		return nil, errors.New("ptvjobs: jobs array is empty")
	}

	items := make([]rfs.ExtractedItem, 0, len(jobs))
	for _, job := range jobs {
		if !isRemoteItaly(job) {
			continue
		}
		items = append(items, toItem(job))
	}
	return items, nil
}

// extractJobsPayload concatenates the string segments of every RSC chunk and
// returns the raw JSON value of the "jobs" key.
func extractJobsPayload(page rfs.Page) ([]byte, error) {
	chunks := nextFPush.FindAllSubmatch(page, -1)
	if len(chunks) == 0 {
		return nil, errors.New("no RSC payload found")
	}

	var payload strings.Builder
	for _, chunk := range chunks {
		var parts []json.RawMessage
		if err := json.Unmarshal(chunk[1], &parts); err != nil {
			return nil, fmt.Errorf("decode RSC chunk: %w", err)
		}
		for _, part := range parts {
			var s string
			if err := json.Unmarshal(part, &s); err != nil {
				continue
			}
			payload.WriteString(s)
		}
	}

	data := payload.String()
	i := strings.Index(data, jobsKey)
	if i < 0 {
		return nil, errors.New("jobs key not found in RSC payload")
	}

	var raw json.RawMessage
	dec := json.NewDecoder(strings.NewReader(data[i+len(jobsKey):]))
	if err := dec.Decode(&raw); err != nil {
		return nil, fmt.Errorf("decode jobs value: %w", err)
	}
	return raw, nil
}

// isRemoteItaly reports whether the posting is remote-eligible for Italy:
// either office list contains "italy" (case-insensitive).
func isRemoteItaly(job personioJob) bool {
	offices := append([]string{job.MainOffice}, job.AdditionalOffices...)
	for _, office := range offices {
		if strings.Contains(strings.ToLower(office), italyTerm) {
			return true
		}
	}
	return false
}

// toItem shapes a posting into an rss item. The posting carries no publication
// date, so PubDate stays nil and rfs falls back to its first-seen timestamp.
func toItem(job personioJob) rfs.ExtractedItem {
	offices := append([]string{job.MainOffice}, job.AdditionalOffices...)
	parts := make([]string, 0, 3)
	if joined := strings.Join(offices, ", "); joined != "" {
		parts = append(parts, joined)
	}
	if job.Schedule != "" {
		parts = append(parts, job.Schedule)
	}
	if job.Department != "" {
		parts = append(parts, job.Department)
	}

	return rfs.ExtractedItem{
		GUID:        guidPrefix + job.ID,
		Title:       job.Name,
		Link:        baseURL + "/job/" + job.ID + "?language=en",
		Description: strings.Join(parts, " · "),
	}
}
