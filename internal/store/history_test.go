package store

import "testing"

func TestSalesByDayFillsDaysWithoutSales(t *testing.T) {
	s := newTestStore(t)
	seedSKU(t, s, "SKU-HIST", 100)

	for i, qty := range []int{3, 4} {
		_, err := s.RecordSale(t.Context(), Sale{
			IdempotencyKey: "hist-" + string(rune('a'+i)),
			SKU:            "SKU-HIST",
			Quantity:       qty,
		})
		if err != nil {
			t.Fatalf("RecordSale() error: %v", err)
		}
	}

	history, err := s.SalesByDay(t.Context(), "SKU-HIST", 7)
	if err != nil {
		t.Fatalf("SalesByDay() error: %v", err)
	}

	if len(history) != 7 {
		t.Fatalf("len(history) = %d, want 7", len(history))
	}
	for _, day := range history[:6] {
		if day.Units != 0 {
			t.Errorf("%s units = %d, want 0", day.Day.Format("2006-01-02"), day.Units)
		}
	}
	if today := history[6]; today.Units != 7 {
		t.Errorf("today units = %d, want 7 (3 + 4)", today.Units)
	}
}
