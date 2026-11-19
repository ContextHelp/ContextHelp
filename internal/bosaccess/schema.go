package bosaccess

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

//go:embed schema/*.json
var schemaFS embed.FS

const schemaIDBase = "https://hop.top/bos/contracts/peer/access/v1/schema/"

// Schema names accepted by Validate.
const (
	SchemaSet      = "access-set"
	SchemaAck      = "access-ack"
	SchemaState    = "access-state"
	SchemaError    = "access-error"
	SchemaScenario = "access-scenario"
)

var (
	schemaOnce sync.Once
	schemaErr  error
	schemas    map[string]*jsonschema.Schema
)

func loadSchemas() {
	c := jsonschema.NewCompiler()
	c.AssertFormat()
	names := []string{SchemaSet, SchemaAck, SchemaState, SchemaError, SchemaScenario}
	for _, n := range names {
		raw, err := schemaFS.ReadFile("schema/" + n + ".schema.json")
		if err != nil {
			schemaErr = err
			return
		}
		doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
		if err != nil {
			schemaErr = fmt.Errorf("bosaccess: %s: %w", n, err)
			return
		}
		if err := c.AddResource(schemaIDBase+n+".schema.json", doc); err != nil {
			schemaErr = err
			return
		}
	}
	schemas = map[string]*jsonschema.Schema{}
	for _, n := range names {
		s, err := c.Compile(schemaIDBase + n + ".schema.json")
		if err != nil {
			schemaErr = fmt.Errorf("bosaccess: %s: %w", n, err)
			return
		}
		schemas[n] = s
	}
}

// Validate checks the JSON document doc against the access schema called
// name (the Schema constants).
func Validate(name string, doc []byte) error {
	schemaOnce.Do(loadSchemas)
	if schemaErr != nil {
		return schemaErr
	}
	s, ok := schemas[name]
	if !ok {
		return fmt.Errorf("bosaccess: no schema %q", name)
	}
	v, err := jsonschema.UnmarshalJSON(bytes.NewReader(doc))
	if err != nil {
		return fmt.Errorf("bosaccess: %s: not JSON: %w", name, err)
	}
	if err := s.Validate(v); err != nil {
		return fmt.Errorf("bosaccess: %s: %w", name, err)
	}
	return nil
}

// ParseSet validates a document against the access-set schema and reads it.
// A document that fails the schema is set_invalid. Reference checks and the
// digest are Apply's: they need the set a delta applies to.
func ParseSet(raw []byte) (Set, error) {
	var s Set
	if err := Validate(SchemaSet, raw); err != nil {
		return s, &Error{Code: CodeSetInvalid, Message: err.Error()}
	}
	if err := json.Unmarshal(raw, &s); err != nil {
		return s, &Error{Code: CodeSetInvalid, Message: err.Error()}
	}
	if s.Mode == ModeFull {
		if err := s.checkRefs(); err != nil {
			return s, &Error{Code: CodeSetInvalid, Message: err.Error()}
		}
		if got := ContentDigest(s); got != s.Digest {
			return s, &Error{Code: CodeDigestMismatch, Message: fmt.Sprintf("digest %s is not the content's %s", s.Digest, got)}
		}
	}
	return s, nil
}
