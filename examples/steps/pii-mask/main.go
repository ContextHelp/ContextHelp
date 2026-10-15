// Command pii-mask is an example dpkms pipeline step (ADR-058): it reads
// a knowledge object as JSON on stdin and writes back, on stdout, the
// text fields it changed with email addresses and phone numbers masked.
//
// Build it into the step directory, then install the directory:
//
//	go build -o examples/steps/pii-mask/pii-mask ./examples/steps/pii-mask
//	dpkms pipeline step install pii-mask --path examples/steps/pii-mask
//
// The patterns are an example, not a complete PII detector.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// object is the part of the knowledge object this step reads and
// rewrites. The output carries only these fields: dpkms keeps every
// field a step leaves out.
type object struct {
	RawContent  string    `json:"raw_content"`
	TextContent string    `json:"text_content,omitempty"`
	InboxNote   string    `json:"inbox_note,omitempty"`
	Summaries   []string  `json:"summaries,omitempty"`
	Sections    []section `json:"sections,omitempty"`
}

type section struct {
	Title    string         `json:"title"`
	Content  string         `json:"content"`
	Order    int            `json:"order"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

type stepError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type result struct {
	Object *object    `json:"object,omitempty"`
	Error  *stepError `json:"error,omitempty"`
}

// config is the step config dpkms writes to DPKMS_STEP_CONFIG_PATH.
type config struct {
	EmailMask string `json:"email_mask"`
	PhoneMask string `json:"phone_mask"`
}

var (
	emailRE = regexp.MustCompile(`[A-Za-z0-9._%+-]+@[A-Za-z0-9-]+(?:\.[A-Za-z0-9-]+)*\.[A-Za-z]{2,}`)
	// A run of digits with the usual separators, optionally +country
	// code; masker keeps only runs of 9 to 15 digits that are not dates.
	phoneRE = regexp.MustCompile(`\+?\(?\d[\d ().-]{6,}\d`)
	dateRE  = regexp.MustCompile(`^\d{4}-\d{1,2}-\d{1,2}`)
)

type masker struct{ cfg config }

func (m masker) mask(s string) string {
	s = emailRE.ReplaceAllString(s, m.cfg.EmailMask)
	return phoneRE.ReplaceAllStringFunc(s, func(c string) string {
		digits := 0
		for _, r := range c {
			if r >= '0' && r <= '9' {
				digits++
			}
		}
		if digits < 9 || digits > 15 || dateRE.MatchString(c) {
			return c
		}
		return m.cfg.PhoneMask
	})
}

func (m masker) apply(o *object) {
	o.RawContent = m.mask(o.RawContent)
	o.TextContent = m.mask(o.TextContent)
	o.InboxNote = m.mask(o.InboxNote)
	for i := range o.Summaries {
		o.Summaries[i] = m.mask(o.Summaries[i])
	}
	for i := range o.Sections {
		o.Sections[i].Title = m.mask(o.Sections[i].Title)
		o.Sections[i].Content = m.mask(o.Sections[i].Content)
	}
}

func loadConfig() (config, error) {
	cfg := config{EmailMask: "[email]", PhoneMask: "[phone]"}
	path := os.Getenv("DPKMS_STEP_CONFIG_PATH")
	if path == "" {
		return cfg, nil
	}
	b, err := os.ReadFile(filepath.Clean(path)) // #nosec G304 G703 -- the config file dpkms names
	if err != nil {
		return cfg, err
	}
	if err := json.Unmarshal(b, &cfg); err != nil {
		return cfg, err
	}
	return cfg, nil
}

func run(stdin io.Reader, stdout io.Writer) int {
	write := func(r result) int {
		if err := json.NewEncoder(stdout).Encode(r); err != nil {
			return 2
		}
		if r.Error != nil {
			return 1
		}
		return 0
	}
	cfg, err := loadConfig()
	if err != nil {
		return write(result{Error: &stepError{Code: "BAD_CONFIG", Message: err.Error()}})
	}
	var o object
	if err := json.NewDecoder(stdin).Decode(&o); err != nil {
		// The message names the problem, never the content.
		return write(result{Error: &stepError{Code: "BAD_INPUT", Message: "input is not a knowledge object: " + strings.SplitN(err.Error(), ":", 2)[0]}})
	}
	masker{cfg: cfg}.apply(&o)
	return write(result{Object: &o})
}

func main() {
	if code := run(os.Stdin, os.Stdout); code != 0 {
		fmt.Fprintln(os.Stderr, "pii-mask: failed")
		os.Exit(code)
	}
}
