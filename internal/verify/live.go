package verify

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// LiveEpoch reads the Date field of the published Release. Any failure means
// the published tree is unknown, never current.
func LiveEpoch(ctx context.Context, baseURL string) (int64, error) {
	req, e := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSuffix(baseURL, "/")+"/dists/stable/Release", nil)
	if e != nil {
		return 0, errors.New("invalid release URL")
	}
	client := &http.Client{Timeout: 15 * time.Second}
	resp, e := client.Do(req)
	if e != nil {
		return 0, errors.New("release request failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("release request failed: %d", resp.StatusCode)
	}
	body, e := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if e != nil {
		return 0, errors.New("release read failed")
	}
	for _, line := range strings.Split(string(body), "\n") {
		value, ok := strings.CutPrefix(line, "Date: ")
		if !ok {
			continue
		}
		date, e := time.Parse(time.RFC1123Z, strings.TrimSpace(value))
		if e != nil {
			return 0, errors.New("invalid release date")
		}
		return date.Unix(), nil
	}
	return 0, errors.New("release date missing")
}
