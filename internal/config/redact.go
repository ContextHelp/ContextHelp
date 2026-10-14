package config

import (
	"errors"
	"fmt"
	"net/url"
	"reflect"
	"regexp"
	"strings"
)

// The `secret` struct tag on a Config field is the single source of
// truth for which values are credentials. Every path that prints config
// (config show in each format, validate and doctor hints, log lines,
// error messages) goes through Redacted, RedactSecret or RedactDSN.
//
//	secret:"true"   the whole value is a credential: a token, a key, a
//	                capability URL
//	secret:"dsn"    a locator (URL, connection string) that may embed a
//	                password; only the embedded credential is masked
//	secret:"keys"   a free-form map: string values under keys whose name
//	                suggests a secret (secretFieldNames) are masked
//	secret:"false"  not a credential, though the name looks like one
//
// A string field whose name contains one of secretFieldNames must carry
// the tag; a test walks the Config type and fails on an untagged one.
const (
	secretTag   = "secret"
	secretWhole = "true"
	secretDSN   = "dsn"
	secretKeys  = "keys"
	secretNone  = "false"
)

// redactMask replaces a credential. redactRevealMin is the shortest
// value whose last four characters are kept after the mask: shorter
// values reveal too large a share of themselves.
const (
	redactMask      = "****"
	redactRevealMin = 16
	redactRevealLen = 4
)

// RedactSecret masks a credential for display: "****" plus the last 4
// characters when the value is at least 16 characters long, "****"
// alone otherwise. An empty value stays empty, so an unset credential
// still reads as unset.
func RedactSecret(s string) string {
	if s == "" {
		return ""
	}
	r := []rune(s)
	if len(r) < redactRevealMin {
		return redactMask
	}
	return redactMask + string(r[len(r)-redactRevealLen:])
}

// dsnSecretParam matches a key=value pair whose key suggests a secret:
// libpq keyword form (password=x, password='a b') and URL query
// parameters (?password=x&...). Built from secretFieldNames so the
// redactor and the plaintext-secret scan share one keyword list.
var dsnSecretParam = regexp.MustCompile(
	`(?i)(\b[a-z_]*(?:` + strings.Join(secretFieldNames, "|") + `)[a-z_]*\s*=\s*)('[^']*'|[^\s&;]+)`)

// urlUserinfo matches the userinfo of a scheme://user[:password]@ URL.
// The password part runs to the last @ before whitespace, so an
// unescaped / or : in it is still masked.
var urlUserinfo = regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9+.-]*://)([^/?#@\s:]*)(?::([^@\s]*))?@`)

// RedactDSN masks the credentials a locator may embed and keeps the
// rest readable: the userinfo of a URL (user:password@ becomes
// user:****@, a bare user@ is masked whole since it is often a token)
// and every secret-looking key=value parameter (password=, sslkey=,
// token=, ...). A value without credentials is returned unchanged.
func RedactDSN(s string) string {
	if s == "" {
		return ""
	}
	if u, err := url.Parse(s); err != nil || u.User != nil {
		s = urlUserinfo.ReplaceAllStringFunc(s, func(m string) string {
			sub := urlUserinfo.FindStringSubmatch(m)
			scheme, user, pass := sub[1], sub[2], sub[3]
			if !strings.Contains(m[len(scheme)+len(user):], ":") {
				return scheme + RedactSecret(user) + "@"
			}
			return scheme + user + ":" + RedactSecret(pass) + "@"
		})
	}
	return dsnSecretParam.ReplaceAllStringFunc(s, func(m string) string {
		sub := dsnSecretParam.FindStringSubmatch(m)
		val := sub[2]
		if strings.HasPrefix(val, "'") {
			return sub[1] + "'" + RedactSecret(strings.Trim(val, "'")) + "'"
		}
		return sub[1] + RedactSecret(val)
	})
}

// RedactURLError masks, as RedactDSN does, the URL every *url.Error in
// err's chain quotes. net/http's client errors repeat the request URL,
// and with it a token= query parameter the configured locator embeds
// (the client already hides a userinfo password). err is changed in
// place and returned. Call it on the error as the client returns it,
// before wrapping: fmt.Errorf renders its message when called, so a
// wrapper made earlier keeps the unmasked URL.
func RedactURLError(err error) error {
	for e := err; ; {
		var ue *url.Error
		if !errors.As(e, &ue) {
			return err
		}
		ue.URL = RedactDSN(ue.URL)
		e = ue.Err
	}
}

// nameLooksSecret reports whether a field or key name contains one of
// secretFieldNames.
func nameLooksSecret(name string) bool {
	return secretKeyword(name) != ""
}

// secretKeyword returns the first secretFieldNames entry found in name,
// or "" when none is.
func secretKeyword(name string) string {
	lower := strings.ToLower(name)
	for _, kw := range secretFieldNames {
		if strings.Contains(lower, kw) {
			return kw
		}
	}
	return ""
}

// Redacted returns a deep copy of cfg with every credential masked as
// its `secret` tag directs. cfg itself is not modified. Print the
// result, never cfg: it is for display only, and writing it back would
// replace each credential with its mask.
func Redacted(cfg *Config) *Config {
	if cfg == nil {
		return nil
	}
	out := *cfg
	redactStruct(reflect.ValueOf(&out).Elem())
	return &out
}

// SecretValues returns every non-empty credential cfg carries, keyed by
// its dot-separated config path (server.auth.static.tokens[0].token).
// A secret:"dsn" locator is listed only when it embeds a credential,
// as the whole locator. Use it to check output for leaks.
func SecretValues(cfg *Config) map[string]string {
	out := map[string]string{}
	if cfg == nil {
		return out
	}
	collectSecrets(reflect.ValueOf(cfg), "", "", out)
	return out
}

// redactStruct masks the tagged fields of the addressable struct v and
// replaces every other exported field with a redacted deep copy, so no
// slice, map or pointer is shared with the source.
func redactStruct(v reflect.Value) {
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		fv := v.Field(i)
		switch f.Tag.Get(secretTag) {
		case secretWhole:
			fv.Set(mapStrings(fv, RedactSecret))
		case secretDSN:
			fv.Set(mapStrings(fv, RedactDSN))
		case secretKeys:
			fv.Set(redactFreeForm(fv, false))
		default:
			fv.Set(cloneRedacted(fv))
		}
	}
}

// cloneRedacted returns a deep copy of v with every tagged field below
// it redacted.
func cloneRedacted(v reflect.Value) reflect.Value {
	switch v.Kind() {
	case reflect.Struct:
		c := reflect.New(v.Type()).Elem()
		c.Set(v)
		redactStruct(c)
		return c
	case reflect.Ptr:
		if v.IsNil() {
			return v
		}
		c := reflect.New(v.Type().Elem())
		c.Elem().Set(cloneRedacted(v.Elem()))
		return c
	case reflect.Slice:
		if v.IsNil() {
			return v
		}
		c := reflect.MakeSlice(v.Type(), v.Len(), v.Len())
		for i := 0; i < v.Len(); i++ {
			c.Index(i).Set(cloneRedacted(v.Index(i)))
		}
		return c
	case reflect.Map:
		if v.IsNil() {
			return v
		}
		c := reflect.MakeMapWithSize(v.Type(), v.Len())
		iter := v.MapRange()
		for iter.Next() {
			c.SetMapIndex(iter.Key(), cloneRedacted(iter.Value()))
		}
		return c
	case reflect.Interface:
		if v.IsNil() {
			return v
		}
		c := reflect.New(v.Type()).Elem()
		c.Set(cloneRedacted(v.Elem()))
		return c
	default:
		return v
	}
}

// mapStrings applies fn to a string (or named string) value, or to each
// element of a string slice, returning a new value of v's type.
func mapStrings(v reflect.Value, fn func(string) string) reflect.Value {
	switch v.Kind() {
	case reflect.String:
		c := reflect.New(v.Type()).Elem()
		c.SetString(fn(v.String()))
		return c
	case reflect.Slice:
		if v.IsNil() {
			return v
		}
		c := reflect.MakeSlice(v.Type(), v.Len(), v.Len())
		for i := 0; i < v.Len(); i++ {
			c.Index(i).Set(mapStrings(v.Index(i), fn))
		}
		return c
	default:
		panic(fmt.Sprintf("config: secret tag on %s field; want string or []string", v.Kind()))
	}
}

// redactFreeForm returns a deep copy of a free-form value (maps,
// slices, interfaces, scalars, as YAML decodes them) with every string
// under a secret-looking key masked. secret is true when an enclosing
// key already looked secret.
func redactFreeForm(v reflect.Value, secret bool) reflect.Value {
	switch v.Kind() {
	case reflect.Interface:
		if v.IsNil() {
			return v
		}
		c := reflect.New(v.Type()).Elem()
		c.Set(redactFreeForm(v.Elem(), secret))
		return c
	case reflect.Map:
		if v.IsNil() {
			return v
		}
		c := reflect.MakeMapWithSize(v.Type(), v.Len())
		iter := v.MapRange()
		for iter.Next() {
			k := fmt.Sprint(iter.Key().Interface())
			c.SetMapIndex(iter.Key(), redactFreeForm(iter.Value(), secret || nameLooksSecret(k)))
		}
		return c
	case reflect.Slice:
		if v.IsNil() {
			return v
		}
		c := reflect.MakeSlice(v.Type(), v.Len(), v.Len())
		for i := 0; i < v.Len(); i++ {
			c.Index(i).Set(redactFreeForm(v.Index(i), secret))
		}
		return c
	case reflect.String:
		if !secret {
			return v
		}
		c := reflect.New(v.Type()).Elem()
		c.SetString(RedactSecret(v.String()))
		return c
	default:
		return v
	}
}

// collectSecrets walks v and records each credential under its config
// path. tag is the `secret` tag of the field v came from.
func collectSecrets(v reflect.Value, path, tag string, out map[string]string) {
	for v.Kind() == reflect.Ptr || v.Kind() == reflect.Interface {
		if v.IsNil() {
			return
		}
		v = v.Elem()
	}
	switch v.Kind() {
	case reflect.Struct:
		t := v.Type()
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			if !f.IsExported() {
				continue
			}
			child := fieldNameFromTag(f)
			if path != "" {
				child = path + "." + child
			}
			collectSecrets(v.Field(i), child, f.Tag.Get(secretTag), out)
		}
	case reflect.Slice:
		for i := 0; i < v.Len(); i++ {
			collectSecrets(v.Index(i), fmt.Sprintf("%s[%d]", path, i), tag, out)
		}
	case reflect.Map:
		iter := v.MapRange()
		for iter.Next() {
			k := fmt.Sprint(iter.Key().Interface())
			collectSecrets(iter.Value(), path+"."+k, mapEntryTag(tag, k), out)
		}
	case reflect.String:
		if isCredential(v.String(), tag) {
			out[path] = v.String()
		}
	}
}

// mapEntryTag is the secret tag a map entry under key inherits from
// its map's tag: a secret-looking key in a free-form map is a whole
// credential, and a whole-credential or free-form map passes its tag on.
func mapEntryTag(tag, key string) string {
	switch {
	case tag == secretKeys && nameLooksSecret(key):
		return secretWhole
	case tag == secretKeys || tag == secretWhole:
		return tag
	default:
		return ""
	}
}

// isCredential reports whether s, held by a field tagged tag, is a
// credential SecretValues lists.
func isCredential(s, tag string) bool {
	switch {
	case s == "":
		return false
	case tag == secretWhole:
		return true
	case tag == secretDSN:
		return RedactDSN(s) != s
	default:
		return false
	}
}
