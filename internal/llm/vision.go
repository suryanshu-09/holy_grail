package llm

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// Figure-type labels for vision-based image classification. Values match
// internal/extraction FigureType* constants; they are duplicated here so
// internal/llm stays importable from internal/extraction without a cycle.
const (
	VisionFigureDiagram    = "diagram"
	VisionFigureGraph      = "graph"
	VisionFigureMathFigure = "math_figure"
	VisionFigureTable      = "table"
	VisionFigureChart      = "chart"
	VisionFigurePhoto      = "photo"
	VisionFigureUnknown    = "unknown"
)

// DefaultVisionModel is used when OPENAI_VISION_MODEL is unset.
const DefaultVisionModel = "gpt-4o-mini"

// VisionModelFromEnv returns the vision model name from OPENAI_VISION_MODEL,
// falling back to DefaultVisionModel when unset or blank.
func VisionModelFromEnv() string {
	if m := strings.TrimSpace(os.Getenv("OPENAI_VISION_MODEL")); m != "" {
		return m
	}
	return DefaultVisionModel
}

// NormalizeVisionFigureType maps a raw model label to one of the
// VisionFigure* constants. Matching is case-insensitive and tolerant of
// common aliases. Unknown or empty input maps to VisionFigureUnknown.
func NormalizeVisionFigureType(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case VisionFigureDiagram, "diagrams":
		return VisionFigureDiagram
	case VisionFigureGraph, "graphs", "plot", "plots":
		return VisionFigureGraph
	case VisionFigureMathFigure, "math", "mathfigure", "math-figure",
		"mathematical figure", "mathematical-figure", "equation", "formula":
		return VisionFigureMathFigure
	case VisionFigureTable, "tables", "tabular":
		return VisionFigureTable
	case VisionFigureChart, "charts", "bar chart", "bar-chart", "barchart",
		"pie chart", "pie-chart", "piechart", "line chart", "histogram":
		return VisionFigureChart
	case VisionFigurePhoto, "photos", "photograph", "photographs", "image", "figure":
		return VisionFigurePhoto
	case VisionFigureUnknown, "", "none", "n/a", "other":
		return VisionFigureUnknown
	default:
		return VisionFigureUnknown
	}
}

// VisionDescribeInput is the input to a VisionDescriber for a single image.
type VisionDescribeInput struct {
	// Name is the image file name (e.g. page-001-img-000.png).
	Name string
	// Page is the 1-based page number the image was extracted from.
	Page int
	// Path is the absolute filesystem path to the image bytes. Describers
	// read (never write) this file; it may be empty when Data is supplied.
	Path string
	// Data holds raw image bytes when Path is unavailable.
	Data []byte
	// Format is the lowercase image extension without dot (png/jpg/...).
	Format string
	// Context is optional surrounding page text to ground the description.
	Context string
}

// VisionDescribeOutput is the result of describing a single image.
type VisionDescribeOutput struct {
	// Description is a concise (1-3 sentence) description of the figure.
	Description string
	// FigureType is one of the VisionFigure* constants.
	FigureType string
	// DescribedBy names the model/backend that produced the description.
	DescribedBy string
}

// VisionDescriber produces a text description plus figure-type classification
// for an image. Implementations must be safe for concurrent use. Callers
// treat errors as "vision unavailable" and continue without descriptions.
type VisionDescriber interface {
	Describe(ctx context.Context, in VisionDescribeInput) (VisionDescribeOutput, error)
}

// NoopVisionDescriber is the graceful fallback used when no vision backend
// is configured (e.g. OPENAI_API_KEY unset). It never calls a network
// service and never returns an error.
type NoopVisionDescriber struct{}

// Describe returns an empty description classified as unknown.
func (NoopVisionDescriber) Describe(_ context.Context, _ VisionDescribeInput) (VisionDescribeOutput, error) {
	return VisionDescribeOutput{FigureType: VisionFigureUnknown, DescribedBy: "noop"}, nil
}

// BuildVisionPrompt builds the deterministic text prompt sent to the vision
// model for an image. The model is instructed to return strict JSON with
// "description" and "figure_type" keys.
func BuildVisionPrompt(in VisionDescribeInput) string {
	var b strings.Builder
	b.WriteString("Describe the educational figure in this image in 1-3 concise sentences. ")
	b.WriteString("Focus on content a student needs to answer questions about it (labels, axes, values, relationships). ")
	b.WriteString("Also classify it with exactly one figure_type from: diagram, graph, math_figure, table, chart, photo, unknown. ")
	b.WriteString("Respond with strict JSON only: {\"description\": \"...\", \"figure_type\": \"...\"}.")
	if strings.TrimSpace(in.Context) != "" {
		b.WriteString(" Page context: ")
		ctx := strings.TrimSpace(in.Context)
		if len(ctx) > 500 {
			ctx = ctx[:500]
		}
		b.WriteString(ctx)
	}
	return b.String()
}

// OpenAIVisionDescriber describes images via OpenAI chat completions with an
// image_url (base64 data URL) part. It never panics; transport, auth, or
// parse failures are returned as errors so the pipeline can skip vision.
type OpenAIVisionDescriber struct {
	APIKey  string
	Model   string
	BaseURL string
	Client  *http.Client
}

// NewOpenAIVisionDescriber constructs an OpenAIVisionDescriber. apiKey is
// required; an empty model falls back to OPENAI_VISION_MODEL env (default
// gpt-4o-mini); an empty baseURL falls back to https://api.openai.com.
func NewOpenAIVisionDescriber(apiKey, model, baseURL string) (*OpenAIVisionDescriber, error) {
	if strings.TrimSpace(apiKey) == "" {
		return nil, fmt.Errorf("openai vision: api key required")
	}
	if strings.TrimSpace(model) == "" {
		model = VisionModelFromEnv()
	}
	if strings.TrimSpace(baseURL) == "" {
		baseURL = "https://api.openai.com"
	}
	return &OpenAIVisionDescriber{
		APIKey:  apiKey,
		Model:   model,
		BaseURL: strings.TrimRight(baseURL, "/"),
		Client:  &http.Client{Timeout: 30 * time.Second},
	}, nil
}

// NewVisionDescriber is the graceful no-key fallback constructor: it returns
// a NoopVisionDescriber when apiKey is blank (never an error), otherwise an
// OpenAIVisionDescriber with OPENAI_VISION_MODEL defaulting. The returned
// value is always non-nil and safe to call.
func NewVisionDescriber(apiKey, model, baseURL string) VisionDescriber {
	if strings.TrimSpace(apiKey) == "" {
		return NoopVisionDescriber{}
	}
	d, err := NewOpenAIVisionDescriber(apiKey, model, baseURL)
	if err != nil {
		return NoopVisionDescriber{}
	}
	return d
}

// visionMIME maps a lowercase format/extension to a data-URL MIME type.
type visionContentPart struct {
	Type     string          `json:"type"`
	Text     string          `json:"text,omitempty"`
	ImageURL *visionImageURL `json:"image_url,omitempty"`
}

type visionImageURL struct {
	URL string `json:"url"`
}

type visionChatMessage struct {
	Role    string              `json:"role"`
	Content []visionContentPart `json:"content"`
}

type visionChatRequest struct {
	Model    string              `json:"model"`
	Messages []visionChatMessage `json:"messages"`
	MaxTok   int                 `json:"max_tokens,omitempty"`
	Temp     float32             `json:"temperature,omitempty"`
}

// visionMIME maps a lowercase format/extension to a data-URL MIME type.
func visionMIME(format string) string {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "jpg", "jpeg":
		return "image/jpeg"
	case "png", "":
		return "image/png"
	case "gif":
		return "image/gif"
	case "webp":
		return "image/webp"
	default:
		return "image/png"
	}
}

// visionDataURL encodes raw image bytes as a base64 data URL.
func visionDataURL(data []byte, format string) string {
	return "data:" + visionMIME(format) + ";base64," + base64.StdEncoding.EncodeToString(data)
}

// loadVisionBytes returns the image bytes for in, preferring in.Data and
// falling back to reading in.Path.
func loadVisionBytes(in VisionDescribeInput) ([]byte, error) {
	if len(in.Data) > 0 {
		return in.Data, nil
	}
	if strings.TrimSpace(in.Path) == "" {
		return nil, fmt.Errorf("openai vision: no image data (empty Data and Path)")
	}
	b, err := os.ReadFile(in.Path)
	if err != nil {
		return nil, fmt.Errorf("openai vision: read image: %w", err)
	}
	if len(b) == 0 {
		return nil, fmt.Errorf("openai vision: empty image file %q", in.Path)
	}
	return b, nil
}

// parseVisionJSON extracts and validates the {description, figure_type}
// payload returned by the vision model. It strips markdown fences, extracts
// the outermost JSON object when wrapped in prose, and normalizes the
// figure type. Unknown fields are rejected.
func parseVisionJSON(s, model string) (VisionDescribeOutput, error) {
	cleaned := strings.TrimSpace(s)
	if strings.HasPrefix(cleaned, "```") {
		if idx := strings.Index(cleaned, "\n"); idx != -1 {
			cleaned = cleaned[idx+1:]
		} else {
			cleaned = strings.TrimPrefix(cleaned, "```json")
			cleaned = strings.TrimPrefix(cleaned, "```JSON")
			cleaned = strings.TrimPrefix(cleaned, "```")
		}
		if idx := strings.LastIndex(cleaned, "```"); idx != -1 {
			cleaned = cleaned[:idx]
		}
		cleaned = strings.TrimSpace(cleaned)
	}
	if !strings.HasPrefix(cleaned, "{") {
		if start, end := strings.Index(cleaned, "{"), strings.LastIndex(cleaned, "}"); start != -1 && end != -1 && end > start {
			cleaned = strings.TrimSpace(cleaned[start : end+1])
		}
	}
	if cleaned == "" {
		return VisionDescribeOutput{}, fmt.Errorf("openai vision: empty response")
	}
	dec := json.NewDecoder(strings.NewReader(cleaned))
	dec.DisallowUnknownFields()
	var payload struct {
		Description string `json:"description"`
		FigureType  string `json:"figure_type"`
	}
	if err := dec.Decode(&payload); err != nil {
		return VisionDescribeOutput{}, fmt.Errorf("openai vision: invalid JSON: %w", err)
	}
	if dec.More() {
		return VisionDescribeOutput{}, fmt.Errorf("openai vision: trailing data after JSON object")
	}
	payload.Description = strings.TrimSpace(payload.Description)
	if payload.Description == "" {
		return VisionDescribeOutput{}, fmt.Errorf("openai vision: description is required")
	}
	if len(payload.Description) > 2000 {
		payload.Description = payload.Description[:2000]
	}
	return VisionDescribeOutput{
		Description: payload.Description,
		FigureType:  NormalizeVisionFigureType(payload.FigureType),
		DescribedBy: model,
	}, nil
}

// Describe sends one chat-completion request with a base64 image_url part and
// returns the parsed description plus normalized figure-type classification.
func (d *OpenAIVisionDescriber) Describe(ctx context.Context, in VisionDescribeInput) (VisionDescribeOutput, error) {
	if d == nil {
		return VisionDescribeOutput{FigureType: VisionFigureUnknown, DescribedBy: "noop"}, fmt.Errorf("openai vision: nil describer")
	}
	if strings.TrimSpace(d.APIKey) == "" {
		return VisionDescribeOutput{FigureType: VisionFigureUnknown}, fmt.Errorf("openai vision: api key required")
	}
	data, err := loadVisionBytes(in)
	if err != nil {
		return VisionDescribeOutput{FigureType: VisionFigureUnknown}, err
	}
	prompt := BuildVisionPrompt(in)
	reqBody := visionChatRequest{
		Model: d.Model,
		Messages: []visionChatMessage{
			{
				Role: "user",
				Content: []visionContentPart{
					{Type: "text", Text: prompt},
					{Type: "image_url", ImageURL: &visionImageURL{URL: visionDataURL(data, in.Format)}},
				},
			},
		},
		MaxTok: 500,
		Temp:   0.0,
	}
	raw, err := json.Marshal(reqBody)
	if err != nil {
		return VisionDescribeOutput{FigureType: VisionFigureUnknown}, fmt.Errorf("openai vision: marshal request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.BaseURL+"/v1/chat/completions", bytes.NewReader(raw))
	if err != nil {
		return VisionDescribeOutput{FigureType: VisionFigureUnknown}, fmt.Errorf("openai vision: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+d.APIKey)

	client := d.Client
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return VisionDescribeOutput{FigureType: VisionFigureUnknown}, fmt.Errorf("openai vision: request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
		return VisionDescribeOutput{FigureType: VisionFigureUnknown}, fmt.Errorf("openai vision: status %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	var out chatResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return VisionDescribeOutput{FigureType: VisionFigureUnknown}, fmt.Errorf("openai vision: decode: %w", err)
	}
	if len(out.Choices) == 0 {
		return VisionDescribeOutput{FigureType: VisionFigureUnknown}, fmt.Errorf("openai vision: no choices in response")
	}
	parsed, err := parseVisionJSON(out.Choices[0].Message.Content, d.Model)
	if err != nil {
		return VisionDescribeOutput{FigureType: VisionFigureUnknown, DescribedBy: d.Model}, err
	}
	return parsed, nil
}
