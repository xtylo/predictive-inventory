package predictor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/anthropics/anthropic-sdk-go/v2"
	"github.com/anthropics/anthropic-sdk-go/v2/option"

	"github.com/xtylo/predictive-inventory/internal/store"
)

const (
	// horizonDays is the period every forecast covers.
	horizonDays = 30
	// maxHistoryDays caps how much history Claude can request per call.
	maxHistoryDays = 90
	// maxTurns bounds the agent loop: each turn is one billed API call.
	maxTurns = 6
	// maxTokens bounds each response. Tool calls are short.
	maxTokens = 1024

	toolSalesHistory   = "get_sales_history"
	toolCurrentStock   = "get_current_stock"
	toolSubmitForecast = "submit_forecast"
)

const systemPrompt = `You are a demand planner for a retail store.
Forecast the total units of one SKU that will sell over the next 30 days.
Always inspect the sales history with the tools before answering. Look for
trend, weekly seasonality and days with zero sales caused by stockouts.
Finish by calling submit_forecast exactly once. Keep the rationale under
40 words.`

// errInvalidInput marks tool input problems Claude can fix by itself.
// They are sent back as tool errors; any other error aborts the forecast.
var errInvalidInput = errors.New("invalid tool input")

// DataSource is what the tools read (implemented by *store.Store).
type DataSource interface {
	SalesByDay(ctx context.Context, sku string, days int) ([]store.DaySales, error)
	GetStock(ctx context.Context, sku string) (int, error)
}

// ClaudeConfig selects the model and where to reach it.
type ClaudeConfig struct {
	APIKey string
	// BaseURL points the client at any Anthropic-compatible gateway, such as
	// Token Harbor. Empty means the Anthropic API itself.
	BaseURL string
	// Model is the ID the endpoint expects, e.g. "claude-haiku-5-5" on the
	// Anthropic API or "mimo-v2.6-flash:free" on Token Harbor.
	Model string
}

// Claude forecasts demand with an agent loop: the model decides which data
// it needs, the service runs the matching Go function and sends the result
// back, until the model submits a forecast through the submit_forecast tool.
//
// It speaks the Anthropic Messages API, so it works with Claude and with
// any other model served through an Anthropic-compatible endpoint.
type Claude struct {
	client anthropic.Client
	model  anthropic.Model
	data   DataSource
}

// NewClaude builds the predictor. opts lets tests point the client at a
// fake server; production passes none.
func NewClaude(cfg ClaudeConfig, data DataSource, opts ...option.RequestOption) *Claude {
	base := []option.RequestOption{option.WithAPIKey(cfg.APIKey)}
	if cfg.BaseURL != "" {
		base = append(base, option.WithBaseURL(cfg.BaseURL))
	}
	// Variadic arguments: opts is a []option.RequestOption, and opts...
	// spreads it back into individual arguments. Options are applied in
	// order, so the ones from tests override the defaults above.
	return &Claude{
		client: anthropic.NewClient(append(base, opts...)...),
		model:  anthropic.Model(cfg.Model),
		data:   data,
	}
}

// tools describes, as JSON Schema, the functions Claude may call.
var tools = []anthropic.ToolUnionParam{
	{OfTool: &anthropic.ToolParam{
		Name: toolSalesHistory,
		Description: anthropic.String("Units sold per day for the SKU over the last N days, " +
			"oldest first and ending today. Days without sales have 0 units."),
		InputSchema: anthropic.ToolInputSchemaParam{
			Properties: map[string]any{
				"days": map[string]any{
					"type":        "integer",
					"minimum":     1,
					"maximum":     maxHistoryDays,
					"description": "How many days of history to return.",
				},
			},
			Required: []string{"days"},
		},
	}},
	{OfTool: &anthropic.ToolParam{
		Name:        toolCurrentStock,
		Description: anthropic.String("Units of the SKU currently in stock."),
		InputSchema: anthropic.ToolInputSchemaParam{Properties: map[string]any{}},
	}},
	{OfTool: &anthropic.ToolParam{
		Name:        toolSubmitForecast,
		Description: anthropic.String("Submit the final 30-day demand forecast. Call it exactly once."),
		InputSchema: anthropic.ToolInputSchemaParam{
			Properties: map[string]any{
				"demand": map[string]any{
					"type":        "integer",
					"minimum":     0,
					"description": "Total units expected to sell in the next 30 days.",
				},
				"rationale": map[string]any{
					"type":        "string",
					"description": "One or two sentences explaining the number.",
				},
			},
			Required: []string{"demand", "rationale"},
		},
	}},
}

// Predict runs the agent loop for one SKU.
func (c *Claude) Predict(ctx context.Context, sku string) (Forecast, error) {
	prompt := fmt.Sprintf("Forecast the demand of SKU %q for the next %d days. Today is %s.",
		sku, horizonDays, time.Now().UTC().Format(time.DateOnly))

	// The API is stateless: every call sends the whole conversation so far.
	messages := []anthropic.MessageParam{
		anthropic.NewUserMessage(anthropic.NewTextBlock(prompt)),
	}

	// Token usage is summed across turns and logged, so the cost of each
	// forecast is visible in the service logs.
	var inputTokens, outputTokens int64
	logUsage := func(turns int) {
		log.Printf("predictor: %s used %d turn(s), %d input / %d output tokens",
			sku, turns, inputTokens, outputTokens)
	}

	for turn := 1; turn <= maxTurns; turn++ {
		msg, err := c.client.Messages.New(ctx, anthropic.MessageNewParams{
			Model:     c.model,
			MaxTokens: maxTokens,
			System:    []anthropic.TextBlockParam{{Text: systemPrompt}},
			Messages:  messages,
			Tools:     tools,
			// "any" forces a tool call on every turn, so Claude can only
			// finish through submit_forecast and never with free text.
			ToolChoice: anthropic.ToolChoiceUnionParam{OfAny: &anthropic.ToolChoiceAnyParam{}},
		})
		if err != nil {
			return Forecast{}, fmt.Errorf("call model (turn %d): %w", turn, err)
		}
		inputTokens += msg.Usage.InputTokens
		outputTokens += msg.Usage.OutputTokens
		modelTokens.WithLabelValues("input").Add(float64(msg.Usage.InputTokens))
		modelTokens.WithLabelValues("output").Add(float64(msg.Usage.OutputTokens))

		// Claude's reply becomes part of the history for the next call.
		messages = append(messages, msg.ToParam())

		var results []anthropic.ContentBlockParamUnion
		for _, block := range msg.Content {
			// Content is a union of block types; AsAny exposes the concrete
			// one so a type assertion can pick out tool calls.
			use, ok := block.AsAny().(anthropic.ToolUseBlock)
			if !ok {
				continue
			}
			input := []byte(use.JSON.Input.Raw())

			if use.Name == toolSubmitForecast {
				forecast, err := parseForecast(input)
				if err == nil {
					logUsage(turn)
					return forecast, nil
				}
				// Invalid answer: tell Claude what is wrong and let it retry.
				results = append(results, anthropic.NewToolResultBlock(use.ID, err.Error(), true))
				continue
			}

			output, err := c.runTool(ctx, sku, use.Name, input)
			switch {
			case errors.Is(err, errInvalidInput):
				results = append(results, anthropic.NewToolResultBlock(use.ID, err.Error(), true))
			case err != nil:
				return Forecast{}, fmt.Errorf("tool %s: %w", use.Name, err)
			default:
				results = append(results, anthropic.NewToolResultBlock(use.ID, output, false))
			}
		}

		if len(results) == 0 {
			logUsage(turn)
			return Forecast{}, fmt.Errorf("model stopped without a forecast (stop reason %q)", msg.StopReason)
		}
		// Tool results travel back to Claude as a user message.
		messages = append(messages, anthropic.NewUserMessage(results...))
	}

	logUsage(maxTurns)
	return Forecast{}, fmt.Errorf("no forecast after %d turns", maxTurns)
}

// runTool executes a data tool and returns its result as JSON text.
func (c *Claude) runTool(ctx context.Context, sku, name string, input []byte) (string, error) {
	switch name {
	case toolSalesHistory:
		var args struct {
			Days int `json:"days"`
		}
		if err := json.Unmarshal(input, &args); err != nil {
			return "", fmt.Errorf("%w: %v", errInvalidInput, err)
		}
		// The schema says 1..90, but model output is untrusted input:
		// validate it like any request body.
		if args.Days < 1 || args.Days > maxHistoryDays {
			return "", fmt.Errorf("%w: days must be between 1 and %d", errInvalidInput, maxHistoryDays)
		}

		history, err := c.data.SalesByDay(ctx, sku, args.Days)
		if err != nil {
			return "", err
		}

		type day struct {
			Day   string `json:"day"`
			Units int    `json:"units"`
		}
		days := make([]day, len(history))
		for i, h := range history {
			days[i] = day{Day: h.Day.Format(time.DateOnly), Units: h.Units}
		}
		return toJSON(days)

	case toolCurrentStock:
		stock, err := c.data.GetStock(ctx, sku)
		if err != nil {
			return "", err
		}
		return toJSON(map[string]int{"stock": stock})

	default:
		return "", fmt.Errorf("%w: unknown tool %q", errInvalidInput, name)
	}
}

// parseForecast validates submit_forecast's input.
func parseForecast(input []byte) (Forecast, error) {
	var args struct {
		// A pointer distinguishes "demand missing" (nil) from "demand 0".
		Demand    *int   `json:"demand"`
		Rationale string `json:"rationale"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return Forecast{}, fmt.Errorf("invalid JSON: %v", err)
	}
	if args.Demand == nil {
		return Forecast{}, errors.New("demand is required")
	}
	if *args.Demand < 0 {
		return Forecast{}, fmt.Errorf("demand must be zero or positive, got %d", *args.Demand)
	}
	if args.Rationale == "" {
		return Forecast{}, errors.New("rationale is required")
	}
	return Forecast{Demand: *args.Demand, HorizonDays: horizonDays, Rationale: args.Rationale}, nil
}

func toJSON(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
