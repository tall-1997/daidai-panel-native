package handler

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"daidai-panel/pkg/response"
	"daidai-panel/service"

	"github.com/gin-gonic/gin"
)

var defaultSponsorFeedURL = "https://dumblist.linzixuan.top/"

const sponsorFeedLogInterval = 10 * time.Minute

var sponsorFeedLogState struct {
	mu       sync.Mutex
	lastWarn time.Time
}

type SponsorHandler struct{}

type sponsorFeedItem struct {
	ID        uint    `json:"id,omitempty"`
	Name      string  `json:"name"`
	Amount    float64 `json:"amount"`
	AvatarURL string  `json:"avatar_url"`
	Initial   string  `json:"initial,omitempty"`
	CreatedAt string  `json:"created_at,omitempty"`
	UpdatedAt string  `json:"updated_at,omitempty"`
}

type sponsorFeedSummary struct {
	Sponsors    []sponsorFeedItem `json:"sponsors"`
	Count       int               `json:"count"`
	TotalAmount float64           `json:"total_amount"`
	UpdatedAt   string            `json:"updated_at,omitempty"`
	Unavailable bool              `json:"unavailable,omitempty"`
}

func NewSponsorHandler() *SponsorHandler {
	return &SponsorHandler{}
}

func (h *SponsorHandler) List(c *gin.Context) {
	c.Header("Cache-Control", "no-store")

	feedURL := defaultSponsorFeedURL

	summary, err := fetchSponsorFeed(feedURL)
	if err != nil {
		logSponsorFeedUnavailable()
		fallback := emptySponsorFeed()
		fallback.Unavailable = true
		response.Success(c, gin.H{"data": fallback})
		return
	}
	response.Success(c, gin.H{"data": summary})
}

func (h *SponsorHandler) RegisterRoutes(r *gin.RouterGroup) {
	r.GET("/sponsors", h.List)
}

func emptySponsorFeed() sponsorFeedSummary {
	return sponsorFeedSummary{
		Sponsors:    []sponsorFeedItem{},
		Count:       0,
		TotalAmount: 0,
		UpdatedAt:   "",
	}
}

func logSponsorFeedUnavailable() {
	sponsorFeedLogState.mu.Lock()
	defer sponsorFeedLogState.mu.Unlock()

	now := time.Now()
	if !sponsorFeedLogState.lastWarn.IsZero() && now.Sub(sponsorFeedLogState.lastWarn) < sponsorFeedLogInterval {
		return
	}

	sponsorFeedLogState.lastWarn = now
	log.Printf("sponsor feed unavailable, using fallback response")
}

func fetchSponsorFeed(feedURL string) (sponsorFeedSummary, error) {
	client := service.NewHTTPClient(8 * time.Second)
	req, err := http.NewRequest(http.MethodGet, feedURL, nil)
	if err != nil {
		return sponsorFeedSummary{}, err
	}
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return sponsorFeedSummary{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return sponsorFeedSummary{}, fmt.Errorf("remote status %d", resp.StatusCode)
	}

	var root map[string]json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&root); err != nil {
		return sponsorFeedSummary{}, err
	}

	var summary sponsorFeedSummary
	if raw, ok := root["data"]; ok && len(raw) > 0 && string(raw) != "null" {
		if err := json.Unmarshal(raw, &summary); err != nil {
			return sponsorFeedSummary{}, err
		}
	} else if raw, ok := root["sponsors"]; ok && len(raw) > 0 && string(raw) != "null" {
		if err := json.Unmarshal(raw, &summary.Sponsors); err != nil {
			return sponsorFeedSummary{}, err
		}
		if rawCount, ok := root["count"]; ok && len(rawCount) > 0 && string(rawCount) != "null" {
			_ = json.Unmarshal(rawCount, &summary.Count)
		}
		if rawTotal, ok := root["total_amount"]; ok && len(rawTotal) > 0 && string(rawTotal) != "null" {
			_ = json.Unmarshal(rawTotal, &summary.TotalAmount)
		}
		if rawUpdatedAt, ok := root["updated_at"]; ok && len(rawUpdatedAt) > 0 && string(rawUpdatedAt) != "null" {
			_ = json.Unmarshal(rawUpdatedAt, &summary.UpdatedAt)
		}
	} else {
		body, err := json.Marshal(root)
		if err != nil {
			return sponsorFeedSummary{}, err
		}
		if err := json.Unmarshal(body, &summary); err != nil {
			return sponsorFeedSummary{}, err
		}
	}

	if summary.Sponsors == nil {
		summary.Sponsors = []sponsorFeedItem{}
	}
	if summary.Count == 0 {
		summary.Count = len(summary.Sponsors)
	}

	for i := range summary.Sponsors {
		summary.Sponsors[i].AvatarURL = resolveRemoteURL(feedURL, summary.Sponsors[i].AvatarURL)
		if strings.TrimSpace(summary.Sponsors[i].Initial) == "" {
			summary.Sponsors[i].Initial = sponsorInitial(summary.Sponsors[i].Name)
		}
	}

	return summary, nil
}

func resolveRemoteURL(feedURL, target string) string {
	target = strings.TrimSpace(target)
	if target == "" {
		return ""
	}
	lower := strings.ToLower(target)
	if strings.HasPrefix(lower, "data:") {
		return target
	}

	base, err := url.Parse(feedURL)
	if err != nil {
		return target
	}
	ref, err := url.Parse(target)
	if err != nil {
		return target
	}

	if ref.IsAbs() {
		return normalizeResolvedRemoteURL(base, ref)
	}

	return normalizeResolvedRemoteURL(base, base.ResolveReference(ref))
}

func normalizeResolvedRemoteURL(base, resolved *url.URL) string {
	if base == nil || resolved == nil {
		return ""
	}

	if strings.EqualFold(base.Scheme, "https") &&
		strings.EqualFold(resolved.Scheme, "http") &&
		strings.EqualFold(base.Hostname(), resolved.Hostname()) &&
		base.Hostname() != "" {
		cloned := *resolved
		cloned.Scheme = "https"
		return cloned.String()
	}

	return resolved.String()
}

func sponsorInitial(name string) string {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return "赞"
	}
	r, _ := utf8.DecodeRuneInString(trimmed)
	if r == utf8.RuneError {
		return "赞"
	}
	return string(r)
}
