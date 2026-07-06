package moderation

import "testing"

func TestFirstBlockedTerm(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		values  []string
		wantHit bool
	}{
		{
			name:    "clean content",
			values:  []string{"Need help with login timeout"},
			wantHit: false,
		},
		{
			name:    "english profanity",
			values:  []string{"This is bullshit"},
			wantHit: true,
		},
		{
			name:    "english profanity with separators",
			values:  []string{"f.u.c.k this bug"},
			wantHit: true,
		},
		{
			name:    "chinese profanity",
			values:  []string{"你这是傻逼系统"},
			wantHit: true,
		},
		{
			name:    "chinese profanity with spaces",
			values:  []string{"你这是 傻 b 系统"},
			wantHit: true,
		},
		{
			name:    "abbreviation profanity",
			values:  []string{"你这回复真是 nmsl"},
			wantHit: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, got := FirstBlockedTerm(tt.values...)
			if got != tt.wantHit {
				t.Fatalf("FirstBlockedTerm() hit = %v, want %v", got, tt.wantHit)
			}
		})
	}
}
