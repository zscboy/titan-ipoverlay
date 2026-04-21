package model

import (
	"testing"

	"titan-ipoverlay/ippop/businesspack"
)

func TestDeriveIPPackLabel(t *testing.T) {
	tests := []struct {
		name    string
		success int64
		failure int64
		want    string
	}{
		{name: "unknown", success: 0, failure: 0, want: businesspack.LabelUnknown},
		{name: "early success", success: 2, failure: 0, want: businesspack.LabelAllow},
		{name: "early gray", success: 1, failure: 1, want: businesspack.LabelGray},
		{name: "stable allow", success: 8, failure: 1, want: businesspack.LabelAllow},
		{name: "gray on moderate failure", success: 4, failure: 3, want: businesspack.LabelGray},
		{name: "deny on mostly failed", success: 1, failure: 5, want: businesspack.LabelDeny},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := DeriveIPPackLabel(tt.success, tt.failure); got != tt.want {
				t.Fatalf("DeriveIPPackLabel(%d, %d) = %q, want %q", tt.success, tt.failure, got, tt.want)
			}
		})
	}
}
