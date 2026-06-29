package config

import (
	"errors"
	"testing"

	"github.com/tidwall/gjson"
)

const testCfg = `{
  "experimental": { "clash_api": { "external_controller": "127.0.0.1:9090" } },
  "inbounds": [
    {
      "type": "vless",
      "tag": "reality-in",
      "listen_port": 443,
      "users": [ { "name": "alice", "uuid": "u-alice", "flow": "xtls-rprx-vision" } ],
      "tls": { "server_name": "www.apple.com", "reality": { "private_key": "PRIV", "short_id": ["abcd"] } }
    },
    {
      "type": "hysteria2",
      "tag": "hy2-in",
      "listen_port": 443,
      "users": [ { "name": "alice", "password": "u-alice" } ],
      "obfs": { "type": "salamander", "password": "obfspw" },
      "tls": { "server_name": "www.bing.com" }
    }
  ],
  "outbounds": [ { "type": "direct", "tag": "direct" } ]
}`

func realityFields(name, secret string) []Field {
	return []Field{{"name", name}, {"uuid", secret}, {"flow", "xtls-rprx-vision"}}
}
func hy2Fields(name, secret string) []Field {
	return []Field{{"name", name}, {"password", secret}}
}

func names(t *testing.T, c *Config, tag, field string) []string {
	t.Helper()
	us, err := c.Users(tag, field)
	if err != nil {
		t.Fatalf("Users(%s): %v", tag, err)
	}
	var out []string
	for _, u := range us {
		out = append(out, u.Name+"="+u.Secret)
	}
	return out
}

func TestExtractUsers(t *testing.T) {
	c := New([]byte(testCfg))
	if got := names(t, c, "reality-in", "uuid"); len(got) != 1 || got[0] != "alice=u-alice" {
		t.Fatalf("reality users = %v", got)
	}
	if got := names(t, c, "hy2-in", "password"); len(got) != 1 || got[0] != "alice=u-alice" {
		t.Fatalf("hy2 users = %v", got)
	}
}

func TestAppendPreservesUnknownFields(t *testing.T) {
	c := New([]byte(testCfg))
	if err := c.AppendUser("reality-in", realityFields("bob", "u-bob")); err != nil {
		t.Fatal(err)
	}
	if err := c.AppendUser("hy2-in", hy2Fields("bob", "u-bob")); err != nil {
		t.Fatal(err)
	}

	// users array grew
	if got := names(t, c, "reality-in", "uuid"); len(got) != 2 {
		t.Fatalf("want 2 reality users, got %v", got)
	}
	// the new user is well-formed (flow present)
	if flow := gjson.GetBytes(c.Bytes(), `inbounds.0.users.1.flow`).String(); flow != "xtls-rprx-vision" {
		t.Fatalf("new reality user flow = %q", flow)
	}
	// unrelated fields untouched
	if pk := gjson.GetBytes(c.Bytes(), "inbounds.0.tls.reality.private_key").String(); pk != "PRIV" {
		t.Fatalf("private_key changed: %q", pk)
	}
	if ec := gjson.GetBytes(c.Bytes(), "experimental.clash_api.external_controller").String(); ec != "127.0.0.1:9090" {
		t.Fatalf("experimental block changed: %q", ec)
	}
	if ob := gjson.GetBytes(c.Bytes(), "outbounds.0.type").String(); ob != "direct" {
		t.Fatalf("outbounds changed: %q", ob)
	}
	if !gjson.ValidBytes(c.Bytes()) {
		t.Fatal("result is not valid JSON")
	}
}

func TestAddThenRemoveRestoresUserSet(t *testing.T) {
	c := New([]byte(testCfg))
	_ = c.AppendUser("reality-in", realityFields("bob", "u-bob"))
	_ = c.AppendUser("hy2-in", hy2Fields("bob", "u-bob"))
	if err := c.RemoveUser("reality-in", "bob"); err != nil {
		t.Fatal(err)
	}
	if err := c.RemoveUser("hy2-in", "bob"); err != nil {
		t.Fatal(err)
	}
	if got := names(t, c, "reality-in", "uuid"); len(got) != 1 || got[0] != "alice=u-alice" {
		t.Fatalf("reality after remove = %v", got)
	}
	if got := names(t, c, "hy2-in", "password"); len(got) != 1 || got[0] != "alice=u-alice" {
		t.Fatalf("hy2 after remove = %v", got)
	}
}

func TestRemoveUserDeletesAllDuplicates(t *testing.T) {
	c := New([]byte(testCfg))
	_ = c.AppendUser("reality-in", realityFields("bob", "u-bob-1"))
	_ = c.AppendUser("reality-in", realityFields("bob", "u-bob-2"))

	if err := c.RemoveUser("reality-in", "bob"); err != nil {
		t.Fatal(err)
	}
	for _, got := range names(t, c, "reality-in", "uuid") {
		if got == "bob=u-bob-1" || got == "bob=u-bob-2" {
			t.Fatalf("duplicate bob still present: %v", names(t, c, "reality-in", "uuid"))
		}
	}
	if got := names(t, c, "reality-in", "uuid"); len(got) != 1 || got[0] != "alice=u-alice" {
		t.Fatalf("reality after duplicate remove = %v", got)
	}
}

func TestHasUser(t *testing.T) {
	c := New([]byte(testCfg))
	for _, tc := range []struct {
		name string
		want bool
	}{{"alice", true}, {"charlie", false}} {
		got, err := c.HasUser("reality-in", tc.name)
		if err != nil {
			t.Fatal(err)
		}
		if got != tc.want {
			t.Fatalf("HasUser(%s) = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestRemoveMissingUser(t *testing.T) {
	c := New([]byte(testCfg))
	err := c.RemoveUser("reality-in", "ghost")
	if !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("want ErrUserNotFound, got %v", err)
	}
}

func TestMissingInbound(t *testing.T) {
	c := New([]byte(testCfg))
	_, err := c.Users("nope-in", "uuid")
	if !errors.Is(err, ErrInboundNotFound) {
		t.Fatalf("want ErrInboundNotFound, got %v", err)
	}
}

func TestRealityAndHy2Params(t *testing.T) {
	c := New([]byte(testCfg))
	rp, err := c.RealityParams("reality-in")
	if err != nil {
		t.Fatal(err)
	}
	if rp.Port != 443 || rp.ServerName != "www.apple.com" || rp.PrivateKey != "PRIV" || rp.ShortID != "abcd" {
		t.Fatalf("reality params = %+v", rp)
	}
	if flow := c.UserFlow("reality-in", "alice"); flow != "xtls-rprx-vision" {
		t.Fatalf("UserFlow(alice) = %q", flow)
	}
	if flow := c.UserFlow("reality-in", "ghost"); flow != "" {
		t.Fatalf("UserFlow(ghost) should be empty, got %q", flow)
	}
	hp, err := c.Hy2Params("hy2-in")
	if err != nil {
		t.Fatal(err)
	}
	if hp.Port != 443 || hp.ObfsType != "salamander" || hp.ObfsPass != "obfspw" {
		t.Fatalf("hy2 params = %+v", hp)
	}
}
