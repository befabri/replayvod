package twitch

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

// VideoLookup is the Helix surface LookupVideosByID needs; *Client satisfies
// it, and callers fake it in tests.
type VideoLookup interface {
	GetVideos(ctx context.Context, params *GetVideosParams) ([]Video, Pagination, error)
}

// LookupVideosByID fetches VODs by id in batches of one hundred. Helix answers
// 404 for a whole batch that names an unknown id, so a failed batch falls back
// to one lookup per id; ids Twitch no longer knows are absent from the result.
func LookupVideosByID(ctx context.Context, api VideoLookup, ids []string) (map[string]Video, error) {
	out := make(map[string]Video, len(ids))
	const batch = 100
	for start := 0; start < len(ids); start += batch {
		chunk := ids[start:min(start+batch, len(ids))]
		videos, _, err := api.GetVideos(ctx, &GetVideosParams{ID: chunk})
		if err == nil {
			for _, v := range videos {
				out[v.ID] = v
			}
			continue
		}
		if !IsNotFound(err) {
			return nil, fmt.Errorf("lookup vods: %w", err)
		}
		if len(chunk) == 1 {
			continue
		}
		for _, id := range chunk {
			videos, _, err := api.GetVideos(ctx, &GetVideosParams{ID: []string{id}})
			if err != nil {
				if IsNotFound(err) {
					continue
				}
				return nil, fmt.Errorf("lookup vod %s: %w", id, err)
			}
			for _, v := range videos {
				out[v.ID] = v
			}
		}
	}
	return out, nil
}

// IsNotFound reports whether err is a Helix 404.
func IsNotFound(err error) bool {
	var he *HelixError
	return errors.As(err, &he) && he.Status == http.StatusNotFound
}

// VideoThumbnailURL fills the %{width}x%{height} template Helix returns for a
// VOD thumbnail.
func VideoThumbnailURL(template string, width, height int) string {
	r := strings.NewReplacer("%{width}", strconv.Itoa(width), "%{height}", strconv.Itoa(height))
	return r.Replace(template)
}

// IsVideoThumbnailPlaceholder reports whether a VOD thumbnail URL is the
// still-processing placeholder Twitch serves until it has rendered a frame.
func IsVideoThumbnailPlaceholder(url string) bool {
	return strings.Contains(url, "404_processing")
}
