package api

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// get_image/get_file may return a URL, a NapCat-local path, or base64.
// A failed URL must not mask another usable representation. Local paths only
// work on shared storage; base64 is the cross-host fallback.
func readNapCatResolvedResource(ctx context.Context, resource *napCatGetImageData, limit int64) ([]byte, error) {
	if resource == nil {
		return nil, errors.New("missing media resource")
	}
	var errs []error
	if resource.Base64 != "" {
		raw := strings.TrimPrefix(resource.Base64, "base64://")
		if int64(len(raw)) > (limit+2)/3*4 {
			errs = append(errs, errors.New("media base64 exceeds limit"))
		} else {
			data, err := base64.StdEncoding.DecodeString(raw)
			if err == nil && len(data) > 0 && int64(len(data)) <= limit {
				return data, nil
			}
			errs = append(errs, errors.New("invalid media base64"))
		}
	}
	seen := make(map[string]bool)
	for _, ref := range []string{resource.URL, resource.File, resource.Path} {
		ref = strings.TrimSpace(ref)
		if ref == "" || seen[ref] {
			continue
		}
		seen[ref] = true
		data, err := readNapCatResource(ctx, ref, limit)
		if err == nil {
			return data, nil
		}
		errs = append(errs, err)
	}
	if len(errs) == 0 {
		return nil, errors.New("media resource has no content")
	}
	return nil, errors.Join(errs...)
}

func readNapCatResource(ctx context.Context, ref string, limit int64) ([]byte, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var reader io.ReadCloser
	u, err := url.Parse(ref)
	if err == nil && (u.Scheme == "http" || u.Scheme == "https") {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, ref, nil)
		if err != nil {
			return nil, errors.New("invalid media URL")
		}
		resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
		if err != nil {
			return nil, fmt.Errorf("media HTTP request failed")
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			return nil, fmt.Errorf("media HTTP status %d", resp.StatusCode)
		}
		if resp.ContentLength > limit {
			resp.Body.Close()
			return nil, errors.New("media exceeds limit")
		}
		reader = resp.Body
	} else {
		if strings.Contains(ref, "://") {
			if err != nil || u.Scheme != "file" || (u.Host != "" && u.Host != "localhost") {
				return nil, errors.New("unsupported media URL scheme")
			}
			ref = u.Path
		}
		file, err := os.Open(ref)
		if err != nil {
			return nil, errors.New("NapCat local media unavailable; use base64 or shared storage")
		}
		info, err := file.Stat()
		if err != nil || !info.Mode().IsRegular() || info.Size() > limit {
			file.Close()
			return nil, errors.New("invalid local media file")
		}
		reader = file
	}
	defer reader.Close()
	data, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return nil, err
	}
	if len(data) == 0 || int64(len(data)) > limit {
		return nil, errors.New("invalid media size")
	}
	return data, ctx.Err()
}
