// Package link builds client share links (vless:// for REALITY, hysteria2://)
// and subscriptions from a user plus inbound parameters.
//
// REALITY clients need the public key, which the server config does not store —
// only the private key. DerivePublicKey recovers it via X25519.
package link

import (
	"encoding/base64"
	"fmt"
	"net/url"
	"strings"

	"golang.org/x/crypto/curve25519"

	"github.com/cdj/sbx/internal/model"
)

// DerivePublicKey computes the REALITY public key (base64url, unpadded) from the
// server private key.
func DerivePublicKey(privB64 string) (string, error) {
	priv, err := base64.RawURLEncoding.DecodeString(privB64)
	if err != nil {
		return "", fmt.Errorf("decode reality private key: %w", err)
	}
	if len(priv) != 32 {
		return "", fmt.Errorf("reality private key must be 32 bytes, got %d", len(priv))
	}
	pub, err := curve25519.X25519(priv, curve25519.Basepoint)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(pub), nil
}

// VLESS builds a vless://...security=reality share link.
func VLESS(name, server string, u model.User, p model.RealityParams) (string, error) {
	pbk, err := DerivePublicKey(p.PrivateKey)
	if err != nil {
		return "", err
	}
	flow := p.Flow
	if flow == "" {
		flow = "xtls-rprx-vision"
	}
	q := url.Values{}
	q.Set("encryption", "none")
	q.Set("security", "reality")
	q.Set("type", "tcp")
	q.Set("sni", p.ServerName)
	q.Set("fp", "chrome")
	q.Set("pbk", pbk)
	if p.ShortID != "" {
		q.Set("sid", p.ShortID)
	}
	q.Set("flow", flow)
	return fmt.Sprintf("vless://%s@%s:%d?%s#%s",
		u.Secret, server, p.Port, q.Encode(), url.PathEscape(name)), nil
}

// Hysteria2 builds a hysteria2://... share link.
func Hysteria2(name, server string, u model.User, p model.Hy2Params) string {
	q := url.Values{}
	if p.ServerName != "" {
		q.Set("sni", p.ServerName)
	}
	if p.ObfsType != "" {
		q.Set("obfs", p.ObfsType)
		q.Set("obfs-password", p.ObfsPass)
	}
	if p.Insecure {
		q.Set("insecure", "1")
	}
	return fmt.Sprintf("hysteria2://%s@%s:%d?%s#%s",
		url.QueryEscape(u.Secret), server, p.Port, q.Encode(), url.PathEscape(name))
}

// Subscription returns base64(newline-joined links) — the common subscription
// format consumed by clients.
func Subscription(links []string) string {
	return base64.StdEncoding.EncodeToString([]byte(strings.Join(links, "\n")))
}
