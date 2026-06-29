// Package config performs structure-preserving surgical edits on a sing-box
// config.json. It never unmarshals the whole document (which would drop unknown
// fields and reorder keys); instead it reads with gjson and writes with sjson,
// touching only the managed inbounds' "users" arrays. sing-box itself remains
// the schema authority — this package owns only the edit mechanics.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"

	"github.com/awsl5714/cdj-sbx/internal/model"
)

var (
	// ErrInboundNotFound means no inbound has the requested tag.
	ErrInboundNotFound = errors.New("inbound not found")
	// ErrUserNotFound means the named user is absent from an inbound.
	ErrUserNotFound = errors.New("user not found")
	// ErrUserExists means the named user is already present.
	ErrUserExists = errors.New("user already exists")
)

// Config is an in-memory sing-box config held as raw JSON bytes. Mutations edit
// the bytes in place; file I/O (atomic apply) is the caller's responsibility.
type Config struct {
	raw []byte
}

// Load reads a config from disk.
func Load(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if !gjson.ValidBytes(b) {
		return nil, fmt.Errorf("%s: not valid JSON", path)
	}
	return &Config{raw: b}, nil
}

// New wraps raw JSON bytes (used in tests and by the apply pipeline).
func New(raw []byte) *Config {
	return &Config{raw: raw}
}

// Bytes returns the current raw JSON.
func (c *Config) Bytes() []byte {
	return c.raw
}

// Field is a key/value pair for a new user object. Values are strings (all the
// fields sbx writes — name, uuid, password, flow — are strings). Order is
// preserved so inserted objects produce deterministic, clean git diffs.
type Field struct {
	Key, Val string
}

func (c *Config) locate(tag string) (int, bool) {
	idx := -1
	gjson.GetBytes(c.raw, "inbounds").ForEach(func(k, v gjson.Result) bool {
		if v.Get("tag").String() == tag {
			idx = int(k.Int())
			return false
		}
		return true
	})
	return idx, idx >= 0
}

// HasUser reports whether the named user exists in the tagged inbound.
func (c *Config) HasUser(tag, name string) (bool, error) {
	idx, ok := c.locate(tag)
	if !ok {
		return false, fmt.Errorf("%w: %s", ErrInboundNotFound, tag)
	}
	found := false
	gjson.GetBytes(c.raw, fmt.Sprintf("inbounds.%d.users", idx)).ForEach(func(_, v gjson.Result) bool {
		if v.Get("name").String() == name {
			found = true
			return false
		}
		return true
	})
	return found, nil
}

// Users extracts the managed users of an inbound, reading the secret from
// secretField ("uuid" for vless/reality, "password" for hysteria2).
func (c *Config) Users(tag, secretField string) ([]model.User, error) {
	idx, ok := c.locate(tag)
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrInboundNotFound, tag)
	}
	var users []model.User
	gjson.GetBytes(c.raw, fmt.Sprintf("inbounds.%d.users", idx)).ForEach(func(_, v gjson.Result) bool {
		users = append(users, model.User{
			Name:   v.Get("name").String(),
			Secret: v.Get(secretField).String(),
		})
		return true
	})
	return users, nil
}

// AppendUser appends a user object (built from ordered fields) to the tagged
// inbound's users array.
func (c *Config) AppendUser(tag string, fields []Field) error {
	idx, ok := c.locate(tag)
	if !ok {
		return fmt.Errorf("%w: %s", ErrInboundNotFound, tag)
	}
	obj := buildObject(fields)
	raw, err := sjson.SetRawBytes(c.raw, fmt.Sprintf("inbounds.%d.users.-1", idx), []byte(obj))
	if err != nil {
		return err
	}
	c.raw = raw
	return nil
}

// RemoveUser deletes all users with the given name from the tagged inbound.
func (c *Config) RemoveUser(tag, name string) error {
	idx, ok := c.locate(tag)
	if !ok {
		return fmt.Errorf("%w: %s", ErrInboundNotFound, tag)
	}
	var userIdxs []int
	gjson.GetBytes(c.raw, fmt.Sprintf("inbounds.%d.users", idx)).ForEach(func(k, v gjson.Result) bool {
		if v.Get("name").String() == name {
			userIdxs = append(userIdxs, int(k.Int()))
		}
		return true
	})
	if len(userIdxs) == 0 {
		return fmt.Errorf("%w: %s", ErrUserNotFound, name)
	}
	for i := len(userIdxs) - 1; i >= 0; i-- {
		raw, err := sjson.DeleteBytes(c.raw, fmt.Sprintf("inbounds.%d.users.%d", idx, userIdxs[i]))
		if err != nil {
			return err
		}
		c.raw = raw
	}
	return nil
}

// RealityParams reads the link-relevant fields of a reality inbound.
func (c *Config) RealityParams(tag string) (model.RealityParams, error) {
	idx, ok := c.locate(tag)
	if !ok {
		return model.RealityParams{}, fmt.Errorf("%w: %s", ErrInboundNotFound, tag)
	}
	inb := gjson.GetBytes(c.raw, fmt.Sprintf("inbounds.%d", idx))
	r := inb.Get("tls.reality")
	// Flow is per-user, not an inbound property — the caller fills it via
	// UserFlow for the specific user being linked.
	return model.RealityParams{
		Port:       int(inb.Get("listen_port").Int()),
		ServerName: inb.Get("tls.server_name").String(),
		PrivateKey: r.Get("private_key").String(),
		ShortID:    r.Get("short_id.0").String(),
	}, nil
}

// UserFlow returns the "flow" of the named user in the tagged inbound, or "" if
// the user or the field is absent.
func (c *Config) UserFlow(tag, name string) string {
	idx, ok := c.locate(tag)
	if !ok {
		return ""
	}
	flow := ""
	gjson.GetBytes(c.raw, fmt.Sprintf("inbounds.%d.users", idx)).ForEach(func(_, v gjson.Result) bool {
		if v.Get("name").String() == name {
			flow = v.Get("flow").String()
			return false
		}
		return true
	})
	return flow
}

// Hy2Params reads the link-relevant fields of a hysteria2 inbound.
func (c *Config) Hy2Params(tag string) (model.Hy2Params, error) {
	idx, ok := c.locate(tag)
	if !ok {
		return model.Hy2Params{}, fmt.Errorf("%w: %s", ErrInboundNotFound, tag)
	}
	inb := gjson.GetBytes(c.raw, fmt.Sprintf("inbounds.%d", idx))
	return model.Hy2Params{
		Port:       int(inb.Get("listen_port").Int()),
		ServerName: inb.Get("tls.server_name").String(),
		ObfsType:   inb.Get("obfs.type").String(),
		ObfsPass:   inb.Get("obfs.password").String(),
		Insecure:   true, // v1 assumes a self-signed hysteria2 cert
	}, nil
}

func buildObject(fields []Field) string {
	var b strings.Builder
	b.WriteByte('{')
	for i, f := range fields {
		if i > 0 {
			b.WriteByte(',')
		}
		kb, _ := json.Marshal(f.Key)
		vb, _ := json.Marshal(f.Val)
		b.Write(kb)
		b.WriteByte(':')
		b.Write(vb)
	}
	b.WriteByte('}')
	return b.String()
}
