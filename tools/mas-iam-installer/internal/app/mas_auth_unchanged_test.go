package app

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestIDPCfgUnchanged(t *testing.T) {
	orig := idpCfgSettle
	idpCfgSettle = time.Millisecond
	t.Cleanup(func() { idpCfgSettle = orig })

	seq := func(gens ...int64) func() (int64, error) {
		i := 0
		return func() (int64, error) {
			g := gens[i]
			if i < len(gens)-1 {
				i++
			}
			return g, nil
		}
	}
	cases := []struct {
		name   string
		before int64
		read   func() (int64, error)
		want   bool
	}{
		{"same both reads", 3, seq(3, 3), true},
		{"bumped by the PUT", 3, seq(4, 4), false},
		{"bumped after the PUT returned", 3, seq(3, 4), false},
		{"did not exist before", 0, seq(1, 1), false},
		{"read fails", 3, func() (int64, error) { return 0, errors.New("forbidden") }, false},
	}
	for _, tc := range cases {
		if got := idpCfgUnchanged(context.Background(), tc.before, tc.read); got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}
