package application

import (
	"testing"

	"github.com/barber-appointment/catalog-service/internal/domain"
)

func TestServiceInputValidationUsesExactDecimalAndMinutes(t *testing.T) {
	validInput := domain.ServiceInput{Name: "Cut", DurationMinutes: 30, Price: "25.50", Currency: "TRY"}
	if !valid(validInput) {
		t.Fatal("expected valid exact decimal price")
	}
	for _, input := range []domain.ServiceInput{
		{Name: "Cut", DurationMinutes: 30, Price: "25.999", Currency: "TRY"},
		{Name: "Cut", DurationMinutes: -1, Price: "25.00", Currency: "TRY"},
		{Name: "Cut", DurationMinutes: 30, BufferAfterMinutes: -1, Price: "25.00", Currency: "TRY"},
		{Name: "Cut", DurationMinutes: 30, Price: "25.00", Currency: "try"},
	} {
		if valid(input) {
			t.Fatalf("expected invalid input: %+v", input)
		}
	}
}
