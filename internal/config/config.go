package config

import (
	"errors"
	"net/url"
	"reflect"
	"strings"

	"github.com/BurntSushi/toml"
	"pkgs.moraxyc.com/internal/safe"
)

type Repository struct {
	BaseURL        string   `toml:"base_url"`
	Suite          string   `toml:"suite"`
	Codename       string   `toml:"codename"`
	Components     []string `toml:"components"`
	Architectures  []string `toml:"architectures"`
	ValidUntilDays int      `toml:"valid_until_days"`
}
type Config struct {
	Repository Repository `toml:"repository"`
	GitHub     struct {
		Organization string `toml:"organization"`
	} `toml:"github"`
}

type tomlSchema struct {
	children map[string]*tomlSchema
	open     bool
}

func schemaForTOMLType(t reflect.Type) *tomlSchema {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() == reflect.Interface || t.Kind() == reflect.Map {
		return &tomlSchema{open: true}
	}
	if t.Kind() == reflect.Slice || t.Kind() == reflect.Array {
		return schemaForTOMLType(t.Elem())
	}
	if t.Kind() != reflect.Struct {
		return &tomlSchema{}
	}
	s := &tomlSchema{children: map[string]*tomlSchema{}}
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if field.PkgPath != "" && !field.Anonymous {
			continue
		}
		name := strings.Split(field.Tag.Get("toml"), ",")[0]
		if name == "-" {
			continue
		}
		fieldType := field.Type
		for fieldType.Kind() == reflect.Pointer {
			fieldType = fieldType.Elem()
		}
		if name == "" && field.Anonymous && fieldType.Kind() == reflect.Struct {
			mergeTOMLSchema(s, schemaForTOMLType(field.Type))
			continue
		}
		if name == "" {
			name = field.Name
		}
		s.children[name] = schemaForTOMLType(field.Type)
	}
	return s
}

func mergeTOMLSchema(dst, src *tomlSchema) {
	if src.open {
		dst.open = true
	}
	for name, child := range src.children {
		dst.children[name] = child
	}
}

func unknownTOMLKey(md *toml.MetaData, out any) string {
	t := reflect.TypeOf(out)
	if t == nil {
		return ""
	}
	schema := schemaForTOMLType(t)
	for _, key := range md.Keys() {
		node := schema
		for _, part := range key {
			if node.open {
				break
			}
			child, ok := node.children[part]
			if !ok {
				return key.String()
			}
			node = child
		}
	}
	return ""
}

func Decode(name string, out any, required ...[]string) error {
	b, e := safe.Regular(name)
	if e != nil {
		return e
	}
	m, e := toml.Decode(string(b), out)
	if e != nil {
		var parseErr toml.ParseError
		if errors.As(e, &parseErr) && strings.Contains(parseErr.Message, "already") {
			return errors.New("duplicate TOML field: " + parseErr.LastKey)
		}
		return errors.New("invalid TOML")
	}
	// BurntSushi/toml accepts case variants, but configuration keys must match exactly.
	if key := unknownTOMLKey(&m, out); key != "" {
		return errors.New("unknown TOML field: " + key)
	}
	for _, k := range required {
		if !m.IsDefined(k...) {
			return errors.New("missing TOML field: " + strings.Join(k, "."))
		}
	}
	return nil
}
func LoadRepositoryConfig(name string) (Config, error) {
	var c Config
	fields := [][]string{{"github", "organization"}}
	for _, s := range []string{"base_url", "suite", "codename", "components", "architectures", "valid_until_days"} {
		fields = append(fields, []string{"repository", s})
	}
	if e := Decode(name, &c, fields...); e != nil {
		return c, e
	}
	return c, c.Validate()
}
func (c Config) Validate() error {
	r := c.Repository
	u, e := url.Parse(r.BaseURL)
	if e != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Path != "/deb" || u.RawPath != "" {
		return errors.New("base_url must be an HTTPS /deb URL without authentication, query or fragment")
	}
	if r.Suite != "stable" || r.Codename != "stable" || len(r.Components) != 1 || r.Components[0] != "main" {
		return errors.New("only stable/main is supported")
	}
	seen := map[string]bool{}
	for _, a := range r.Architectures {
		if !safe.Identifier(a) || a == "all" || seen[a] {
			return errors.New("invalid or duplicate architecture")
		}
		seen[a] = true
	}
	if len(seen) == 0 || r.ValidUntilDays < 1 || r.ValidUntilDays > 30 || !safe.Identifier(c.GitHub.Organization) {
		return errors.New("invalid repository policy")
	}
	return nil
}
