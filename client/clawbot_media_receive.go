package client

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Wire fields verified against openclaw-weixin 2.4.9 src/api/types.ts.
type ClawBotVoiceItem struct {
	Media         *ClawBotCDNMedia `json:"media,omitempty"`
	Text          string           `json:"text,omitempty"`
	EncodeType    int              `json:"encode_type,omitempty"`
	BitsPerSample int              `json:"bits_per_sample,omitempty"`
	SampleRate    int              `json:"sample_rate,omitempty"`
	Playtime      int              `json:"playtime,omitempty"`
}
type ClawBotFileItem struct {
	Media    *ClawBotCDNMedia `json:"media,omitempty"`
	FileName string           `json:"file_name,omitempty"`
	MD5      string           `json:"md5,omitempty"`
	Len      string           `json:"len,omitempty"`
}
type ClawBotVideoItem struct {
	Media      *ClawBotCDNMedia `json:"media,omitempty"`
	VideoSize  int              `json:"video_size,omitempty"`
	PlayLength int              `json:"play_length,omitempty"`
	VideoMD5   string           `json:"video_md5,omitempty"`
	ThumbMedia *ClawBotCDNMedia `json:"thumb_media,omitempty"`
}

func HasClawBotAttachments(msg ClawBotIncomingMessage) bool {
	for _, item := range msg.ItemList {
		if item.Type >= 3 && item.Type <= 5 {
			return true
		}
	}
	return false
}

// DownloadMediaItem preserves the original bytes, including unsupported voice codecs.
func (c *ClawBotClient) DownloadMediaItem(ctx context.Context, item ClawBotIncomingMessageItem) ([]byte, string, string, error) {
	var media *ClawBotCDNMedia
	var kind, name, digest, size string
	switch {
	case item.Type == 3 && item.VoiceItem != nil:
		media, kind, name = item.VoiceItem.Media, "audio", "voice.bin"
		if extension, ok := map[int]string{1: "pcm", 2: "adpcm", 4: "speex", 5: "amr", 6: "silk", 7: "mp3", 8: "ogg"}[item.VoiceItem.EncodeType]; ok {
			name = "voice." + extension
		}
	case item.Type == 4 && item.FileItem != nil:
		media, kind, name = item.FileItem.Media, "file", item.FileItem.FileName
		digest, size = item.FileItem.MD5, item.FileItem.Len
		if name == "" {
			name = "file.bin"
		}
	case item.Type == 5 && item.VideoItem != nil:
		media, kind, name = item.VideoItem.Media, "video", "video.mp4"
		digest = item.VideoItem.VideoMD5
	default:
		return nil, "", "", errors.New("unsupported media item")
	}
	if media == nil || strings.TrimSpace(media.AESKey) == "" {
		return nil, kind, name, errors.New("missing media encryption key")
	}
	if size != "" {
		n, err := strconv.ParseInt(size, 10, 64)
		if err != nil || n < 0 || n > clawBotMediaMaxBytes {
			return nil, kind, name, errors.New("invalid media size")
		}
	}
	data, err := c.downloadClawBotMedia(ctx, media, "")
	if err != nil {
		return nil, kind, name, err
	}
	if size != "" && size != strconv.Itoa(len(data)) {
		return nil, kind, name, errors.New("media size mismatch")
	}
	if digest != "" {
		sum := md5.Sum(data)
		if !strings.EqualFold(hex.EncodeToString(sum[:]), digest) {
			return nil, kind, name, errors.New("media checksum mismatch")
		}
	}
	return data, kind, name, nil
}

func validateClawBotMediaURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Fragment != "" {
		return errors.New("invalid media URL")
	}
	if ip := net.ParseIP(u.Hostname()); ip != nil && !publicMediaIP(ip) {
		return errors.New("private media URL rejected")
	}
	if strings.EqualFold(u.Hostname(), "localhost") {
		return errors.New("private media URL rejected")
	}
	return nil
}
func publicMediaIP(ip net.IP) bool {
	return ip.IsGlobalUnicast() && !ip.IsPrivate() && !ip.IsLoopback() && !ip.IsLinkLocalUnicast()
}

func (c *ClawBotClient) downloadClawBotMedia(ctx context.Context, media *ClawBotCDNMedia, hexKey string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	if media == nil {
		return nil, errors.New("missing media")
	}
	if media.EncryptType != 0 && media.EncryptType != 1 {
		return nil, errors.New("unsupported media encryption")
	}
	key, err := parseClawBotImageAESKey(&ClawBotImageItem{Media: media, AESKey: hexKey})
	if err != nil {
		return nil, errors.New("invalid media encryption key")
	}
	if media.EncryptType == 1 && len(key) == 0 {
		return nil, errors.New("missing media encryption key")
	}
	raw := strings.TrimSpace(media.FullURL)
	if raw == "" {
		if strings.TrimSpace(media.EncryptQueryParam) == "" {
			return nil, errors.New("missing media URL")
		}
		raw = buildClawBotCDNDownloadURL(defaultClawBotCDNBaseURL, media.EncryptQueryParam)
	}
	if err := validateClawBotMediaURL(raw); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return nil, errors.New("invalid media request")
	}
	hc := *c.httpClient
	hc.CheckRedirect = func(r *http.Request, via []*http.Request) error { return errors.New("media redirects rejected") }
	// Pin each connection to a checked DNS answer to prevent DNS rebinding/SSRF.
	transport := hc.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	if transport, ok := transport.(*http.Transport); ok {
		tr := transport.Clone()
		tr.Proxy = nil
		// A custom TLS dialer would bypass the checked DialContext below.
		tr.DialTLSContext = nil
		tr.DialTLS = nil
		tr.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(address)
			if err != nil {
				return nil, err
			}
			ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
			if err != nil {
				return nil, err
			}
			for _, ip := range ips {
				if !publicMediaIP(ip.IP) {
					return nil, errors.New("private media address rejected")
				}
			}
			if len(ips) == 0 {
				return nil, errors.New("missing media address")
			}
			return (&net.Dialer{Timeout: 15 * time.Second}).DialContext(ctx, network, net.JoinHostPort(ips[0].IP.String(), port))
		}
		hc.Transport = tr
		defer tr.CloseIdleConnections()
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, clawBotSafeRequestError(req, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("media download HTTP %d", resp.StatusCode)
	}
	if resp.ContentLength > clawBotMediaMaxBytes+16 {
		return nil, errors.New("media too large")
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, clawBotMediaMaxBytes+17))
	if err != nil {
		return nil, errors.New("media download interrupted")
	}
	if len(data) == 0 || len(data) > clawBotMediaMaxBytes+16 {
		return nil, errors.New("invalid media size")
	}
	if len(key) > 0 {
		data, err = decryptClawBotAESECB(data, key)
		if err != nil {
			return nil, err
		}
	}
	if len(data) == 0 || len(data) > clawBotMediaMaxBytes {
		return nil, errors.New("invalid media size")
	}
	return data, nil
}
