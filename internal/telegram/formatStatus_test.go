package telegram

import (
	"strings"
	"testing"

	"github.com/amantur/krugo-bot/internal/tasks"
)

func TestFormatStatusRecommendationOnce(t *testing.T) {
	req := &tasks.Request{
		ID:             "KRUGOSVET-1",
		Status:         tasks.StatusHermesResponded,
		Recommendation: "Договор №q9wynhz1pi4tpvh — изменения выполнены\n\nПлатёж создан:\n  Сумма: 500 USD",
	}

	out := formatStatus(req)

	count := strings.Count(out, "Рекомендация")
	if count != 1 {
		t.Errorf("expected 'Рекомендация' to appear exactly 1 time, got %d\noutput:\n%s", count, out)
	}

	// The recommendation body should also not be duplicated.
	bodyCount := strings.Count(out, "Платёж создан")
	if bodyCount != 1 {
		t.Errorf("expected recommendation body to appear 1 time, got %d\noutput:\n%s", bodyCount, out)
	}
}

func TestFormatStatusNoRecommendation(t *testing.T) {
	req := &tasks.Request{
		ID:     "KRUGOSVET-2",
		Status: tasks.StatusReceived,
	}

	out := formatStatus(req)

	if strings.Contains(out, "Рекомендация") {
		t.Errorf("expected no 'Рекомендация' when empty, got:\n%s", out)
	}
}
