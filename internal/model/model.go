// Package model holds the small typed view of the parts of a sing-box config
// that sbx manages. It deliberately does NOT model the whole schema — the raw
// config is edited surgically (see internal/config); these structs carry only
// what the invariant and link layers need.
package model

// User is a managed user: a name plus its bearer secret. For the reality
// inbound the secret is the VLESS uuid; for the hysteria2 inbound it is the
// password. Invariant I1 requires them to be equal for the same user.
type User struct {
	Name   string `json:"name"`
	Secret string `json:"secret"`
}

// InboundRef identifies a managed inbound by tag and its index in the
// config's inbounds array.
type InboundRef struct {
	Tag   string
	Index int
}

// RealityParams carries the reality inbound fields needed to build a client
// vless:// link. PrivateKey is the server key; the client-facing public key is
// derived from it (see internal/link).
type RealityParams struct {
	Port       int
	ServerName string // SNI
	PrivateKey string
	ShortID    string
	Flow       string
}

// Hy2Params carries the hysteria2 inbound fields needed to build a hy2:// link.
type Hy2Params struct {
	Port       int
	ServerName string // SNI for the link; from cert or overridden
	ObfsType   string
	ObfsPass   string
	Insecure   bool
}
