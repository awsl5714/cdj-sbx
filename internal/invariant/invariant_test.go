package invariant

import (
	"testing"

	"github.com/cdj/sbx/internal/model"
)

func u(name, secret string) model.User { return model.User{Name: name, Secret: secret} }

func TestCheck(t *testing.T) {
	tests := []struct {
		name    string
		reality []model.User
		hy2     []model.User
		wantID  string // "" means no violation
	}{
		{name: "empty sets ok", reality: nil, hy2: nil, wantID: ""},
		{
			name:    "matching single user ok",
			reality: []model.User{u("alice", "s1")},
			hy2:     []model.User{u("alice", "s1")},
			wantID:  "",
		},
		{
			name:    "matching multi ok regardless of order",
			reality: []model.User{u("alice", "s1"), u("bob", "s2")},
			hy2:     []model.User{u("bob", "s2"), u("alice", "s1")},
			wantID:  "",
		},
		{
			name:    "missing in hy2 is I1",
			reality: []model.User{u("alice", "s1"), u("bob", "s2")},
			hy2:     []model.User{u("alice", "s1")},
			wantID:  "I1",
		},
		{
			name:    "secret mismatch is I1",
			reality: []model.User{u("alice", "s1")},
			hy2:     []model.User{u("alice", "DIFFERENT")},
			wantID:  "I1",
		},
		{
			name:    "extra in hy2 is I1",
			reality: []model.User{u("alice", "s1")},
			hy2:     []model.User{u("alice", "s1"), u("eve", "s9")},
			wantID:  "I1",
		},
		{
			name:    "duplicate secret is I2",
			reality: []model.User{u("alice", "dup"), u("bob", "dup")},
			hy2:     []model.User{u("alice", "dup"), u("bob", "dup")},
			wantID:  "I2",
		},
		{
			name:    "duplicate name is I2",
			reality: []model.User{u("alice", "s1"), u("alice", "s2")},
			hy2:     []model.User{u("alice", "s1"), u("alice", "s2")},
			wantID:  "I2",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Check(tt.reality, tt.hy2)
			if tt.wantID == "" {
				if err != nil {
					t.Fatalf("want no violation, got %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("want %s violation, got nil", tt.wantID)
			}
			v, ok := err.(*Violation)
			if !ok {
				t.Fatalf("want *Violation, got %T: %v", err, err)
			}
			if v.ID != tt.wantID {
				t.Fatalf("want %s, got %s (%s)", tt.wantID, v.ID, v.Detail)
			}
		})
	}
}
