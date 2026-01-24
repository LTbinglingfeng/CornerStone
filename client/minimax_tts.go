package client

import (
	"bytes"
	"context"
	"cornerstone/logging"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	minimaxTTSRequestTimeout = 2 * time.Minute
)

type MinimaxTTSClient struct {
	BaseURL    string
	APIKey     string
	HTTPClient *http.Client
}

func NewMinimaxTTSClient(baseURL, apiKey string) *MinimaxTTSClient {
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		baseURL = "https://api.minimaxi.com"
	}
	baseURL = strings.TrimRight(baseURL, "/")
	return &MinimaxTTSClient{
		BaseURL:    baseURL,
		APIKey:     strings.TrimSpace(apiKey),
		HTTPClient: newHTTPClient(),
	}
}

type minimaxTTSRequest struct {
	Model         string              `json:"model"`
	Text          string              `json:"text"`
	Stream        bool                `json:"stream"`
	VoiceSetting  minimaxVoiceSetting `json:"voice_setting,omitempty"`
	AudioSetting  minimaxAudioSetting `json:"audio_setting,omitempty"`
	LanguageBoost string              `json:"language_boost,omitempty"`
}

type minimaxVoiceSetting struct {
	VoiceID string  `json:"voice_id"`
	Speed   float64 `json:"speed,omitempty"`
}

type minimaxAudioSetting struct {
	SampleRate int    `json:"sample_rate,omitempty"`
	Bitrate    int    `json:"bitrate,omitempty"`
	Format     string `json:"format"`
	Channel    int    `json:"channel,omitempty"`
}

type minimaxTTSResponse struct {
	Data struct {
		Audio  string `json:"audio"`
		Status int    `json:"status"`
	} `json:"data"`
	BaseResp struct {
		StatusCode int    `json:"status_code"`
		StatusMsg  string `json:"status_msg"`
	} `json:"base_resp"`
	TraceID string `json:"trace_id"`
}

func (c *MinimaxTTSClient) TextToMP3(ctx context.Context, model, text, voiceID string, speed float64, languageBoost string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, minimaxTTSRequestTimeout)
	defer cancel()

	reqBody := minimaxTTSRequest{
		Model:  strings.TrimSpace(model),
		Text:   text,
		Stream: false,
		VoiceSetting: minimaxVoiceSetting{
			VoiceID: strings.TrimSpace(voiceID),
			Speed:   speed,
		},
		AudioSetting: minimaxAudioSetting{
			SampleRate: 32000,
			Bitrate:    128000,
			Format:     "mp3",
			Channel:    1,
		},
		LanguageBoost: strings.TrimSpace(languageBoost),
	}

	bodyBytes, errMarshal := json.Marshal(reqBody)
	if errMarshal != nil {
		return nil, fmt.Errorf("marshal request: %w", errMarshal)
	}

	httpReq, errCreate := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/v1/t2a_v2", bytes.NewReader(bodyBytes))
	if errCreate != nil {
		return nil, fmt.Errorf("create request: %w", errCreate)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.APIKey)

	resp, errDo := c.HTTPClient.Do(httpReq)
	if errDo != nil {
		return nil, fmt.Errorf("do request: %w", errDo)
	}
	defer func() {
		if errClose := resp.Body.Close(); errClose != nil {
			logging.Warnf("close minimax tts body error: %v", errClose)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyBytes))
		return nil, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(body))
	}

	var parsed minimaxTTSResponse
	if errDecode := json.NewDecoder(resp.Body).Decode(&parsed); errDecode != nil {
		return nil, fmt.Errorf("decode response: %w", errDecode)
	}

	if parsed.BaseResp.StatusCode != 0 {
		msg := strings.TrimSpace(parsed.BaseResp.StatusMsg)
		if msg == "" {
			msg = "unknown error"
		}
		return nil, fmt.Errorf("API error: %s", msg)
	}

	audioHex := strings.TrimSpace(parsed.Data.Audio)
	if audioHex == "" {
		return nil, fmt.Errorf("empty audio payload")
	}

	audioBytes, errDecodeHex := hex.DecodeString(audioHex)
	if errDecodeHex != nil {
		return nil, fmt.Errorf("decode audio hex: %w", errDecodeHex)
	}

	return audioBytes, nil
}
