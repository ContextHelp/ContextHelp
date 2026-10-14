package config

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestRedactSecret(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ""},
		{"abc", "****"},
		{"fifteen-chars!!", "****"},      // 15: below the reveal threshold
		{"sixteen-chars!!!", "****s!!!"}, // 16: mask + last 4
		{"sk-ant-api03-abcdefghijWXYZ", "****WXYZ"},
		{"ééééééééééééééééabcd", "****abcd"}, // runes, not bytes
	}
	for _, tc := range cases {
		if got := RedactSecret(tc.in); got != tc.want {
			t.Errorf("RedactSecret(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestRedactDSN(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ""},
		{"/var/lib/ctxt/db.sqlite", "/var/lib/ctxt/db.sqlite"},
		{"https://peer.example.net:8080/api", "https://peer.example.net:8080/api"},
		{"postgres://jad:testpw@db:5432/testdb", "postgres://jad:****@db:5432/testdb"},
		{"postgres://jad:a-long-test-password-WXYZ@db/testdb", "postgres://jad:****WXYZ@db/testdb"},
		{"https://ghp_abcdefghijklmnop1234@github.com/x", "https://****1234@github.com/x"},
		{"postgres://db/ctxt?sslmode=disable&password=testpw", "postgres://db/ctxt?sslmode=disable&password=****"},
		{"host=db user=jad password=testpw dbname=ctxt", "host=db user=jad password=**** dbname=ctxt"},
		{"host=db password='two words here' dbname=ctxt", "host=db password='****' dbname=ctxt"},
		// Unparseable URL with an unescaped / in the password.
		{"postgres://jad:test/pw%zz@db/testdb", "postgres://jad:****@db/testdb"},
	}
	for _, tc := range cases {
		if got := RedactDSN(tc.in); got != tc.want {
			t.Errorf("RedactDSN(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// isStringLeaf reports whether t is a kind the secret tag applies to.
func isStringLeaf(t reflect.Type) bool {
	return t.Kind() == reflect.String ||
		(t.Kind() == reflect.Slice && t.Elem().Kind() == reflect.String)
}

// isFreeForm reports whether t is a map whose values are untyped
// (interface) or strings: keys there are user data, not schema.
func isFreeForm(t reflect.Type) bool {
	return t.Kind() == reflect.Map &&
		(t.Elem().Kind() == reflect.Interface || t.Elem().Kind() == reflect.String)
}

// locatorKeywords name fields that hold a URL or connection string,
// which may embed a password.
var locatorKeywords = []string{"url", "dsn", "endpoint"}

// walkConfigFields calls fn for every exported field reachable from
// Config's type, with its dot path.
func walkConfigFields(fn func(path string, f reflect.StructField)) {
	seen := map[reflect.Type]bool{}
	var walk func(t reflect.Type, path string)
	walk = func(t reflect.Type, path string) {
		for t.Kind() == reflect.Ptr || t.Kind() == reflect.Slice || t.Kind() == reflect.Map {
			t = t.Elem()
		}
		if t.Kind() != reflect.Struct || seen[t] {
			return
		}
		seen[t] = true
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			if !f.IsExported() {
				continue
			}
			p := fieldNameFromTag(f)
			if path != "" {
				p = path + "." + p
			}
			fn(p, f)
			walk(f.Type, p)
		}
	}
	walk(reflect.TypeOf(Config{}), "")
}

// Every field whose name suggests a credential must say whether it is
// one. Adding a token/secret/password/key field without a secret tag
// fails here, before it can reach config show unredacted.
func TestSecretTagCoversSecretLookingFields(t *testing.T) {
	walkConfigFields(func(path string, f reflect.StructField) {
		tag, tagged := f.Tag.Lookup(secretTag)
		if tagged {
			switch tag {
			case secretWhole, secretDSN:
				if !isStringLeaf(f.Type) {
					t.Errorf("%s: secret:%q on %s; want string or []string", path, tag, f.Type)
				}
			case secretKeys:
				if !isFreeForm(f.Type) {
					t.Errorf("%s: secret:%q on %s; want a free-form map", path, tag, f.Type)
				}
			case secretNone:
			default:
				t.Errorf("%s: unknown secret tag %q", path, tag)
			}
			return
		}
		name := f.Name + " " + fieldNameFromTag(f)
		switch {
		case isStringLeaf(f.Type) && nameLooksSecret(name):
			t.Errorf("%s (%s): name suggests a credential (%s); tag it secret:\"true\" or secret:\"false\"",
				path, f.Name, secretKeyword(name))
		case isStringLeaf(f.Type) && containsAny(name, locatorKeywords):
			t.Errorf("%s (%s): locator may embed a password; tag it secret:\"dsn\", secret:\"true\" or secret:\"false\"",
				path, f.Name)
		case isFreeForm(f.Type):
			t.Errorf("%s (%s): free-form map may hold credentials; tag it secret:\"keys\" or secret:\"false\"",
				path, f.Name)
		}
	})
}

// The postgres connection string lives in storage.path; it must stay
// tagged even though its name looks harmless.
func TestStoragePathIsDSN(t *testing.T) {
	f, _ := reflect.TypeOf(StorageConfig{}).FieldByName("Path")
	if got := f.Tag.Get(secretTag); got != secretDSN {
		t.Fatalf("storage.path secret tag = %q, want %q", got, secretDSN)
	}
}

func containsAny(s string, subs []string) bool {
	lower := strings.ToLower(s)
	for _, sub := range subs {
		if strings.Contains(lower, sub) {
			return true
		}
	}
	return false
}

// populate fills every string reachable from v with a unique value
// derived from its path. Tagged secrets get a long marker; dsn fields a
// URL with a password; free-form maps one secret and one plain key.
// Slices get one element, maps one entry, pointers a fresh value.
func populate(v reflect.Value, path, tag string) {
	switch v.Kind() {
	case reflect.Ptr:
		v.Set(reflect.New(v.Type().Elem()))
		populate(v.Elem(), path, tag)
	case reflect.Struct:
		t := v.Type()
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			if !f.IsExported() {
				continue
			}
			populate(v.Field(i), path+"."+fieldNameFromTag(f), f.Tag.Get(secretTag))
		}
	case reflect.Slice:
		s := reflect.MakeSlice(v.Type(), 1, 1)
		populate(s.Index(0), path+"[0]", tag)
		v.Set(s)
	case reflect.Map:
		m := reflect.MakeMap(v.Type())
		if tag == secretKeys {
			m.SetMapIndex(reflect.ValueOf("api_key"), reflect.ValueOf("FREEFORM-SECRET"+path+"-WXYZ"))
			m.SetMapIndex(reflect.ValueOf("region"), reflect.ValueOf("plain"+path))
		} else if v.Type().Key().Kind() == reflect.String {
			e := reflect.New(v.Type().Elem()).Elem()
			populate(e, path+".k", tag)
			m.SetMapIndex(reflect.ValueOf("k").Convert(v.Type().Key()), e)
		}
		v.Set(m)
	case reflect.String:
		switch tag {
		case secretWhole:
			v.SetString("SECRET" + path + "-WXYZ")
		case secretDSN:
			v.SetString("postgres://u:testDSNPASS" + path + "@host/testdb")
		default:
			v.SetString("plain" + path)
		}
	}
}

func populatedConfig() *Config {
	cfg := &Config{}
	populate(reflect.ValueOf(cfg).Elem(), "", "")
	return cfg
}

// Redacted masks every tagged field and leaves every other field
// byte-identical, without touching its input.
func TestRedactedMasksEveryTaggedField(t *testing.T) {
	cfg := populatedConfig()
	orig := populatedConfig()
	red := Redacted(cfg)

	if !reflect.DeepEqual(cfg, orig) {
		t.Fatal("Redacted modified its input")
	}

	secrets := SecretValues(cfg)
	if len(secrets) == 0 {
		t.Fatal("SecretValues found no credentials in a fully populated config")
	}
	// Every tagged field the type declares is populated, so each shows
	// up in SecretValues.
	walkConfigFields(func(path string, f reflect.StructField) {
		tag := f.Tag.Get(secretTag)
		if tag != secretWhole && tag != secretDSN && tag != secretKeys {
			return
		}
		found := false
		for p := range secrets {
			if strings.Contains(stripIndex(p), path) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("tagged field %s missing from SecretValues", path)
		}
	})

	var redStrings []string
	walkStrings(reflect.ValueOf(red), "", func(_, val string) { redStrings = append(redStrings, val) })
	all := strings.Join(redStrings, "\n")
	for path, sec := range secrets {
		if strings.Contains(all, sec) {
			t.Errorf("%s: raw value %q survives redaction", path, sec)
		}
	}
	for _, marker := range []string{"DSNPASS", "FREEFORM-SECRET", "SECRET."} {
		if strings.Contains(all, marker) {
			t.Errorf("redacted config still contains %q", marker)
		}
	}

	// Untagged fields are unchanged: compare every string not under a
	// tagged field.
	plain := map[string]string{}
	walkStrings(reflect.ValueOf(cfg), "", func(p, val string) {
		if strings.HasPrefix(val, "plain") {
			plain[p] = val
		}
	})
	walkStrings(reflect.ValueOf(red), "", func(p, val string) {
		if want, ok := plain[p]; ok && val != want {
			t.Errorf("%s: non-secret changed %q -> %q", p, want, val)
		}
		delete(plain, p)
	})
	for p := range plain {
		t.Errorf("%s: non-secret dropped by Redacted", p)
	}

	// Masks keep the last 4 characters of long values.
	if !strings.Contains(all, "****WXYZ") {
		t.Error("want ****WXYZ masks in redacted config")
	}
}

func stripIndex(p string) string {
	for {
		i := strings.Index(p, "[")
		if i < 0 {
			return p
		}
		j := strings.Index(p[i:], "]")
		if j < 0 {
			return p
		}
		p = p[:i] + p[i+j+1:]
	}
}

// The structured encodings config show uses carry no raw credential.
func TestRedactedEncodings(t *testing.T) {
	cfg := populatedConfig()
	secrets := SecretValues(cfg)
	red := Redacted(cfg)
	enc := map[string]func(any) ([]byte, error){
		"json": json.Marshal,
		"yaml": yaml.Marshal,
		"text": func(v any) ([]byte, error) { return []byte(fmt.Sprintf("%+v", v)), nil },
	}
	for name, marshal := range enc {
		b, err := marshal(red)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		for path, sec := range secrets {
			if strings.Contains(string(b), sec) {
				t.Errorf("%s encoding leaks %s", name, path)
			}
		}
	}
}

func TestRedactedNil(t *testing.T) {
	if Redacted(nil) != nil {
		t.Error("Redacted(nil) != nil")
	}
	if len(SecretValues(nil)) != 0 {
		t.Error("SecretValues(nil) not empty")
	}
}

// A malformed endpoint URL is quoted in the load error with its
// password masked; url.Parse's own message, which repeats the raw URL,
// is not.
func TestValidateEndpointURLRedactsPassword(t *testing.T) {
	for _, raw := range []string{
		"ftp://jad:testpw-endpoint-pass@host",   // bad scheme
		"https://jad:testpw-endpoint-pass@",     // missing host
		"https://jad:testpw-endpoint-pass@h:x/", // parse error (bad port)
	} {
		err := validateEndpointURL(raw)
		if err == nil {
			t.Fatalf("validateEndpointURL(%q) = nil, want error", raw)
		}
		if strings.Contains(err.Error(), "testpw") {
			t.Errorf("error leaks the password: %v", err)
		}
		if !strings.Contains(err.Error(), "jad:****") {
			t.Errorf("error should still quote the URL with the password masked: %v", err)
		}
	}
}
