package predictor

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/anthropics/anthropic-sdk-go/v2/option"

	"github.com/xtylo/predictive-inventory/internal/store"
)

// fakeAnthropic is an httptest server that answers /v1/messages with
// canned responses, in order, and records every request body. Tests never
// reach the real API, cost nothing and are deterministic.
type fakeAnthropic struct {
	t         *testing.T
	responses []string

	mu       sync.Mutex
	requests []string
}

func (f *fakeAnthropic) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/v1/messages" {
		f.t.Errorf("unexpected path %s", r.URL.Path)
	}
	body, _ := io.ReadAll(r.Body)

	f.mu.Lock()
	f.requests = append(f.requests, string(body))
	// Once the script runs out, keep repeating the last response.
	next := f.responses[min(len(f.requests), len(f.responses))-1]
	f.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(w, next)
}

// toolUse builds a Messages API response containing one tool call.
func toolUse(id, name, input string) string {
	return fmt.Sprintf(`{
		"id": "msg_%[1]s", "type": "message", "role": "assistant", "model": "test-model",
		"content": [{"type": "tool_use", "id": "toolu_%[1]s", "name": %[2]q, "input": %[3]s}],
		"stop_reason": "tool_use", "stop_sequence": null,
		"usage": {"input_tokens": 10, "output_tokens": 5}
	}`, id, name, input)
}

type fakeData struct{}

func (fakeData) SalesByDay(_ context.Context, _ string, days int) ([]store.DaySales, error) {
	history := make([]store.DaySales, days)
	start := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	for i := range history {
		history[i] = store.DaySales{Day: start.AddDate(0, 0, i), Units: 8}
	}
	return history, nil
}

func (fakeData) GetStock(context.Context, string) (int, error) { return 120, nil }

// newTestClaude wires a Claude predictor to a fake API server.
func newTestClaude(t *testing.T, responses ...string) (*Claude, *fakeAnthropic) {
	t.Helper()
	fake := &fakeAnthropic{t: t, responses: responses}
	server := httptest.NewServer(fake)
	t.Cleanup(server.Close)

	c := NewClaude(ClaudeConfig{APIKey: "test-key", BaseURL: server.URL, Model: "test-model"},
		fakeData{},
		option.WithMaxRetries(0),
	)
	return c, fake
}

func TestClaudeReadsHistoryThenSubmits(t *testing.T) {
	c, fake := newTestClaude(t,
		toolUse("1", toolSalesHistory, `{"days": 3}`),
		toolUse("2", toolSubmitForecast, `{"demand": 240, "rationale": "Flat 8 units a day."}`),
	)

	got, err := c.Predict(t.Context(), "SKU-1")
	if err != nil {
		t.Fatalf("Predict() error: %v", err)
	}
	want := Forecast{Demand: 240, HorizonDays: horizonDays, Rationale: "Flat 8 units a day."}
	if got != want {
		t.Errorf("Predict() = %+v, want %+v", got, want)
	}

	if len(fake.requests) != 2 {
		t.Fatalf("API calls = %d, want 2", len(fake.requests))
	}
	// The second call must carry the tool result with the real history.
	second := fake.requests[1]
	for _, want := range []string{`"tool_result"`, `"toolu_1"`, `2026-11-03`} {
		if !strings.Contains(second, want) {
			t.Errorf("second request does not contain %s", want)
		}
	}
}

func TestClaudeCorrectsInvalidForecast(t *testing.T) {
	c, fake := newTestClaude(t,
		toolUse("1", toolSubmitForecast, `{"demand": -5, "rationale": "oops"}`),
		toolUse("2", toolSubmitForecast, `{"demand": 50, "rationale": "Fixed."}`),
	)

	got, err := c.Predict(t.Context(), "SKU-1")
	if err != nil {
		t.Fatalf("Predict() error: %v", err)
	}
	if got.Demand != 50 {
		t.Errorf("demand = %d, want 50", got.Demand)
	}
	// The rejected answer goes back flagged as an error so Claude retries.
	if !strings.Contains(fake.requests[1], `"is_error":true`) {
		t.Error("second request does not flag the tool result as an error")
	}
}

func TestClaudeGivesUpAfterMaxTurns(t *testing.T) {
	// A model that keeps asking for stock and never answers.
	c, fake := newTestClaude(t, toolUse("1", toolCurrentStock, `{}`))

	if _, err := c.Predict(t.Context(), "SKU-1"); err == nil {
		t.Fatal("Predict() returned no error, want one")
	}
	if len(fake.requests) != maxTurns {
		t.Errorf("API calls = %d, want %d", len(fake.requests), maxTurns)
	}
}

func TestParseForecast(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{name: "valid", input: `{"demand": 10, "rationale": "ok"}`},
		{name: "zero demand is valid", input: `{"demand": 0, "rationale": "no sales"}`},
		{name: "missing demand", input: `{"rationale": "ok"}`, wantErr: true},
		{name: "negative demand", input: `{"demand": -1, "rationale": "ok"}`, wantErr: true},
		{name: "missing rationale", input: `{"demand": 10}`, wantErr: true},
		{name: "not json", input: `nope`, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseForecast([]byte(tt.input))
			if (err != nil) != tt.wantErr {
				t.Errorf("parseForecast(%s) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
		})
	}
}
