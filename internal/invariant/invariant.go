// Package invariant verifies the semantic constraints that `sing-box check`
// (a schema validator) structurally cannot: that the reality and hysteria2
// inbounds describe the same set of users, and that secrets are unique.
package invariant

import (
	"fmt"

	"github.com/awsl5714/cdj-sbx/internal/model"
)

// Violation is a semantic invariant failure. ID is "I1" or "I2", matching the
// machine-readable error kinds surfaced by the CLI.
type Violation struct {
	ID     string
	Detail string
}

func (v *Violation) Error() string {
	return fmt.Sprintf("invariant %s violated: %s", v.ID, v.Detail)
}

// Check verifies:
//
//	I1: reality and hy2 describe the same user set
//	    (name <-> name, reality.uuid == hy2.password)
//	I2: within each inbound, names and secrets are unique
//
// It returns a *Violation on the first failure, or nil if all hold.
func Check(reality, hy2 []model.User) error {
	if v := checkUnique(reality); v != nil {
		return v
	}
	if v := checkUnique(hy2); v != nil {
		return v
	}
	if v := checkEqual(reality, hy2); v != nil {
		return v
	}
	return nil
}

func checkUnique(users []model.User) *Violation {
	names := make(map[string]bool, len(users))
	secrets := make(map[string]bool, len(users))
	for _, u := range users {
		if names[u.Name] {
			return &Violation{ID: "I2", Detail: fmt.Sprintf("duplicate name %q", u.Name)}
		}
		if secrets[u.Secret] {
			return &Violation{ID: "I2", Detail: fmt.Sprintf("duplicate secret for name %q", u.Name)}
		}
		names[u.Name] = true
		secrets[u.Secret] = true
	}
	return nil
}

func checkEqual(reality, hy2 []model.User) *Violation {
	rm := toMap(reality)
	hm := toMap(hy2)
	for name, secret := range rm {
		hs, ok := hm[name]
		if !ok {
			return &Violation{ID: "I1", Detail: fmt.Sprintf("user %q in reality-in but missing in hy2-in", name)}
		}
		if hs != secret {
			return &Violation{ID: "I1", Detail: fmt.Sprintf("user %q: reality uuid != hy2 password", name)}
		}
	}
	for name := range hm {
		if _, ok := rm[name]; !ok {
			return &Violation{ID: "I1", Detail: fmt.Sprintf("user %q in hy2-in but missing in reality-in", name)}
		}
	}
	return nil
}

func toMap(users []model.User) map[string]string {
	m := make(map[string]string, len(users))
	for _, u := range users {
		m[u.Name] = u.Secret
	}
	return m
}
