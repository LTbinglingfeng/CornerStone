package client

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"mime"
	"net/http"
	"reflect"
	"strings"
	"time"

	"google.golang.org/genai"
	sdk "google.golang.org/genai/interactions"
	"google.golang.org/genai/interactions/models/components"
	ix "google.golang.org/genai/interactions/models/interactions"
	"google.golang.org/genai/interactions/models/operations"
	"google.golang.org/genai/interactions/retry"
)

const imageGenerationTimeout = 5 * time.Minute

type GeneratedImageResult struct {
	MIMEType string
	Data     []byte
	Text     string
}

// ImageGenerationConfig configures a single stateless image generation.
// Empty fields use model defaults. MIMEType may be empty or image/jpeg (the
// only explicit output MIME supported by the installed Interactions schema).
// There is deliberately no batch count, continuation, tool, or persistence option.
type ImageGenerationConfig struct {
	AspectRatio string
	Size        string
	MIMEType    string
}

// GeminiImageClient uses the Gemini Developer Interactions API, not Imagen.
type GeminiImageClient struct {
	BaseURL    string
	APIKey     string
	HTTPClient *http.Client
	// APIVersion overrides the version suffix in BaseURL when nonempty.
	APIVersion string
}

func NewGeminiImageClient(baseURL, apiKey string) *GeminiImageClient {
	if baseURL == "" {
		baseURL = "https://generativelanguage.googleapis.com/v1beta"
	}
	return &GeminiImageClient{BaseURL: strings.TrimSuffix(baseURL, "/"), APIKey: apiKey, HTTPClient: newHTTPClient()}
}

func imageResponseFormat(cfg *ImageGenerationConfig) (ix.ImageResponseFormat, error) {
	f := ix.ImageResponseFormat{Delivery: ix.ImageResponseFormatDeliveryInline.ToPointer()}
	if cfg == nil {
		return f, nil
	}
	if cfg.AspectRatio != "" {
		ratio := ix.ImageResponseFormatAspectRatio(cfg.AspectRatio)
		if !ratio.IsExact() {
			return f, fmt.Errorf("unsupported image aspect ratio: %q", cfg.AspectRatio)
		}
		f.AspectRatio = &ratio
	}
	if cfg.Size != "" {
		size := ix.ImageResponseFormatImageSize(cfg.Size)
		if !size.IsExact() {
			return f, fmt.Errorf("unsupported image size: %q", cfg.Size)
		}
		f.ImageSize = &size
	}
	if cfg.MIMEType != "" {
		if cfg.MIMEType != "image/jpeg" {
			return f, fmt.Errorf("unsupported image output MIME %q: Interactions currently supports explicit image/jpeg only", cfg.MIMEType)
		}
		f.MimeType = ix.ImageResponseFormatMimeTypeImageJpeg.ToPointer()
	}
	return f, nil
}

// GenerateImage makes exactly one Create call, without retries or continuation.
// It returns the first inline image and all accompanying model text. Additional
// images, if emitted by the model in that same response, do not trigger requests.
// Credentials must be explicit: never silently discover keys/ADC from the host.
func (c *GeminiImageClient) GenerateImage(ctx context.Context, model, prompt string, cfg *ImageGenerationConfig) (*GeneratedImageResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(model) == "" {
		return nil, fmt.Errorf("model is required")
	}
	if strings.TrimSpace(prompt) == "" {
		return nil, fmt.Errorf("prompt is required")
	}
	if strings.TrimSpace(c.APIKey) == "" {
		return nil, fmt.Errorf("image API key is required (environment credential discovery is disabled)")
	}
	format, err := imageResponseFormat(cfg)
	if err != nil {
		return nil, err
	}
	base, version := normalizeGeminiBaseURL(c.BaseURL)
	if c.APIVersion != "" {
		version = c.APIVersion
	}
	if version == "" {
		version = "v1beta"
	}
	opts := []sdk.SDKOption{
		sdk.WithSecurity(components.Security{APIKey: &c.APIKey}),
		sdk.WithAPIVersion(version),
		// SDK defaults retry connection failures, 429 and 5xx up to four times.
		// Generation is billable and not idempotent; never inherit that policy.
		sdk.WithRetryConfig(retry.Config{Strategy: "none"}),
	}
	if base != "" {
		opts = append(opts, sdk.WithServerURL(base))
	}
	if c.HTTPClient != nil {
		opts = append(opts, sdk.WithClient(c.HTTPClient))
	}
	upstream := sdk.New(opts...)
	ctx, cancel := context.WithTimeout(ctx, imageGenerationTimeout)
	defer cancel()
	body := operations.NewCreateInteractionRequestBody(ix.CreateModelInteraction{
		Model: ix.Model(model), Input: sdk.Pointer(ix.NewInteractionsInput(prompt)),
		Store: sdk.Bool(false), Stream: sdk.Bool(false),
		ResponseFormat: sdk.Pointer(ix.NewCreateModelInteractionResponseFormat(ix.NewResponseFormat(format))),
	})
	resp, err := upstream.Interactions.Create(ctx, operations.CreateInteractionRequest{Body: body})
	if err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("image interaction: %w", ctx.Err())
		}
		return nil, fmt.Errorf("image interaction: %w", err)
	}
	if resp == nil || resp.Interaction == nil {
		return nil, fmt.Errorf("empty image interaction response")
	}
	return parseImageInteraction(resp.Interaction)
}

func parseImageInteraction(response *ix.Interaction) (*GeneratedImageResult, error) {
	var texts []string
	var images []*ix.ImageContent
	for _, step := range response.Steps {
		if step.ModelOutputStep == nil {
			continue
		}
		output := step.ModelOutputStep
		if output.Error != nil {
			details, _ := json.Marshal(output.Error)
			return nil, fmt.Errorf("image generation refused or failed: %s", details)
		}
		for _, content := range output.Content {
			if content.TextContent != nil {
				texts = append(texts, content.TextContent.Text)
			}
			if content.ImageContent != nil {
				images = append(images, content.ImageContent)
			}
		}
	}
	text := strings.Join(texts, "\n")
	if text == "" && response.OutputText != nil {
		text = *response.OutputText
	}
	if response.Status != ix.InteractionStatusCompleted || len(response.Errors) > 0 {
		details, _ := json.Marshal(response.Errors)
		return nil, fmt.Errorf("image interaction not completed (status=%s, errors=%s, text=%q)", response.Status, details, text)
	}
	if len(images) == 0 && response.OutputImage != nil {
		images = append(images, response.OutputImage)
	}
	if len(images) == 0 {
		return nil, fmt.Errorf("empty image response (possible refusal); text=%q", text)
	}
	var first *GeneratedImageResult
	for _, image := range images {
		if image.MimeType == nil {
			return nil, fmt.Errorf("image response missing MIME type")
		}
		mt, _, err := mime.ParseMediaType(string(*image.MimeType))
		if err != nil || !strings.HasPrefix(mt, "image/") {
			return nil, fmt.Errorf("invalid image response MIME: %q", *image.MimeType)
		}
		if image.Data == nil || *image.Data == "" {
			return nil, fmt.Errorf("empty inline image data (URI delivery is not supported)")
		}
		data, err := base64.StdEncoding.DecodeString(*image.Data)
		if err != nil {
			return nil, fmt.Errorf("invalid image base64: %w", err)
		}
		if len(data) == 0 {
			return nil, fmt.Errorf("empty decoded image data")
		}
		if first == nil {
			first = &GeneratedImageResult{MIMEType: mt, Data: data, Text: text}
		}
	}
	return first, nil
}

// Deprecated: Imagen GenerateImages is no longer supported. Use GenerateImage.
// This method never sends a request, including when NumberOfImages exceeds one.
func (c *GeminiImageClient) GenerateImages(ctx context.Context, model, prompt string, cfg *genai.GenerateImagesConfig) (*genai.GenerateImagesResponse, error) {
	return nil, fmt.Errorf("GenerateImages (Imagen) is deprecated and disabled; use GenerateImage for one Interactions image per call")
}

// Deprecated: Use GenerateImage with ImageGenerationConfig. Only ImageConfig's
// aspect ratio, size and MIME are mapped. Unsupported options fail locally;
// AutomaticContinuation and ResponseModalities are obsolete and ignored.
func (c *GeminiImageClient) GenerateContentImage(ctx context.Context, model, prompt string, cfg *genai.GenerateContentConfig) (*GeneratedImageResult, error) {
	own := &ImageGenerationConfig{}
	if cfg != nil {
		remaining := *cfg
		remaining.AutomaticContinuation = nil
		remaining.ResponseModalities = nil
		remaining.ImageConfig = nil
		if !reflect.DeepEqual(remaining, genai.GenerateContentConfig{}) {
			return nil, fmt.Errorf("unsupported legacy image generation options; use ImageGenerationConfig")
		}
		if cfg.ImageConfig != nil {
			img := *cfg.ImageConfig
			own.AspectRatio, own.Size, own.MIMEType = img.AspectRatio, img.ImageSize, img.OutputMIMEType
			img.AspectRatio, img.ImageSize, img.OutputMIMEType = "", "", ""
			if !reflect.DeepEqual(img, genai.ImageConfig{}) {
				return nil, fmt.Errorf("unsupported legacy ImageConfig options; use ImageGenerationConfig")
			}
		}
	}
	return c.GenerateImage(ctx, model, prompt, own)
}
