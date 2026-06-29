package link

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"net/url"
	"strings"
	"testing"

	"github.com/cdj/sbx/internal/model"
)

// RFC 7748 §6.1 X25519 test vector validates the curve operation underlying
// REALITY public-key derivation.
func TestDerivePublicKeyRFC7748(t *testing.T) {
	privHex := "77076d0a7318a57d3c16c17251b26645df4c2f87ebc0992ab177fba51db92c2a"
	wantHex := "8520f0098930a754748b7ddcb43ef75a0dbf3a0d26381af4eba4a98eaa9b4e6a"

	priv, _ := hex.DecodeString(privHex)
	privB64 := base64.RawURLEncoding.EncodeToString(priv)

	gotB64, err := DerivePublicKey(privB64)
	if err != nil {
		t.Fatal(err)
	}
	got, err := base64.RawURLEncoding.DecodeString(gotB64)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := hex.DecodeString(wantHex)
	if !bytes.Equal(got, want) {
		t.Fatalf("derived public key = %x, want %x", got, want)
	}
}

func TestDerivePublicKeyBadInput(t *testing.T) {
	if _, err := DerivePublicKey("not-base64!!"); err == nil {
		t.Fatal("want error on bad base64")
	}
	if _, err := DerivePublicKey(base64.RawURLEncoding.EncodeToString([]byte("short"))); err == nil {
		t.Fatal("want error on wrong-length key")
	}
}

func realityParams() model.RealityParams {
	priv, _ := hex.DecodeString("77076d0a7318a57d3c16c17251b26645df4c2f87ebc0992ab177fba51db92c2a")
	return model.RealityParams{
		Port:       443,
		ServerName: "www.apple.com",
		PrivateKey: base64.RawURLEncoding.EncodeToString(priv),
		ShortID:    "abcd",
		Flow:       "xtls-rprx-vision",
	}
}

func TestVLESS(t *testing.T) {
	link, err := VLESS("alice", "203.0.113.10", model.User{Name: "alice", Secret: "u-alice"}, realityParams())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(link, "vless://u-alice@203.0.113.10:443?") {
		t.Fatalf("bad prefix: %s", link)
	}
	u, err := url.Parse(link)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if q.Get("security") != "reality" || q.Get("sni") != "www.apple.com" ||
		q.Get("sid") != "abcd" || q.Get("flow") != "xtls-rprx-vision" {
		t.Fatalf("missing/wrong query params: %v", q)
	}
	if q.Get("pbk") == "" {
		t.Fatal("public key missing")
	}
	if u.Fragment != "alice" {
		t.Fatalf("fragment = %q", u.Fragment)
	}
}

func TestHysteria2(t *testing.T) {
	p := model.Hy2Params{Port: 443, ServerName: "www.bing.com", ObfsType: "salamander", ObfsPass: "obfspw", Insecure: true}
	link := Hysteria2("alice", "203.0.113.10", model.User{Name: "alice", Secret: "u-alice"}, p)
	if !strings.HasPrefix(link, "hysteria2://u-alice@203.0.113.10:443?") {
		t.Fatalf("bad prefix: %s", link)
	}
	u, _ := url.Parse(link)
	q := u.Query()
	if q.Get("obfs") != "salamander" || q.Get("obfs-password") != "obfspw" || q.Get("insecure") != "1" {
		t.Fatalf("missing/wrong query params: %v", q)
	}
}

func TestSubscription(t *testing.T) {
	links := []string{"vless://a", "hysteria2://b"}
	sub := Subscription(links)
	dec, err := base64.StdEncoding.DecodeString(sub)
	if err != nil {
		t.Fatal(err)
	}
	if string(dec) != "vless://a\nhysteria2://b" {
		t.Fatalf("decoded subscription = %q", dec)
	}
}
