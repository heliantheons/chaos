package chaos

import (
	"context"
	"errors"
	"testing"
)

func TestVerifyOptionalDependency(t *testing.T) {
	tests := []struct {
		name   string
		verify func(context.Context) error
		want   bool
	}{
		{
			name: "available",
			verify: func(context.Context) error {
				return nil
			},
			want: true,
		},
		{
			name: "unavailable",
			verify: func(context.Context) error {
				return errors.New("dependency unavailable")
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := verifyOptionalDependency(t.Context(), "test dependency", tt.verify); got != tt.want {
				t.Fatalf("verifyOptionalDependency() = %v, want %v", got, tt.want)
			}
		})
	}
}
