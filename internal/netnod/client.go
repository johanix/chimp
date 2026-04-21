package netnod

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type Client struct {
	baseURL string
	token   string
	http    *http.Client
}

func NewClient(cfg NetnodConfig) *Client {
	return &Client{
		baseURL: cfg.BaseURL,
		token:   cfg.Token,
		http:    &http.Client{Timeout: 60 * time.Second},
	}
}

func (c *Client) do(ctx context.Context, path string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Token "+c.token)
	return c.http.Do(req)
}

// SitesConfigured returns the list of sites the API knows about (eligible,
// not guaranteed to have data).
func (c *Client) SitesConfigured(ctx context.Context) ([]string, error) {
	resp, err := c.do(ctx, "/sites/configured")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("sites/configured: %d %s", resp.StatusCode, string(body))
	}
	var sites []string
	if err := json.NewDecoder(resp.Body).Decode(&sites); err != nil {
		return nil, fmt.Errorf("decoding sites/configured: %w", err)
	}
	return sites, nil
}

// SitesAvailable returns the sites that have statistics at the given minute.
func (c *Client) SitesAvailable(ctx context.Context, t time.Time) ([]string, error) {
	path := fmt.Sprintf("/sites/available/%s", t.UTC().Format("200601021504"))
	resp, err := c.do(ctx, path)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("sites/available %s: %d %s",
			t.UTC().Format(time.RFC3339), resp.StatusCode, string(body))
	}
	var sites []string
	if err := json.NewDecoder(resp.Body).Decode(&sites); err != nil {
		return nil, fmt.Errorf("decoding sites/available: %w", err)
	}
	return sites, nil
}

// FetchResult is the outcome of a /dsc/:site/:time request.
type FetchResult struct {
	Data        []byte // empty when NoData is true
	ContentType string
	NoData      bool // true on HTTP 204
}

// FetchDSC retrieves DSC data for a site at a specific minute.
func (c *Client) FetchDSC(ctx context.Context, site string, t time.Time) (*FetchResult, error) {
	return c.fetchDSC(ctx, site, t.UTC().Format("200601021504"), t)
}

// FetchDSCHour retrieves DSC data for a site for a full hour (used for
// backfill of historical data — one request returns 60 minutes of arrays).
func (c *Client) FetchDSCHour(ctx context.Context, site string, hour time.Time) (*FetchResult, error) {
	return c.fetchDSC(ctx, site, hour.UTC().Format("2006010215"), hour)
}

func (c *Client) fetchDSC(ctx context.Context, site, timeStr string, t time.Time) (*FetchResult, error) {
	path := fmt.Sprintf("/dsc/%s/%s", site, timeStr)
	resp, err := c.do(ctx, path)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("reading body: %w", err)
		}
		ct := resp.Header.Get("Content-Type")
		if ct == "" {
			ct = "application/gzip"
		}
		return &FetchResult{Data: body, ContentType: ct}, nil
	case http.StatusNoContent:
		return &FetchResult{NoData: true}, nil
	default:
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("dsc %s %s: %d %s",
			site, t.UTC().Format(time.RFC3339), resp.StatusCode, string(body))
	}
}
