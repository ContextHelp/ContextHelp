package providers

import (
	"archive/zip"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strings"
)

// docxBodyPart is the main document part of a WordprocessingML package.
const docxBodyPart = "word/document.xml"

// maxDocxBodySize caps how much of the body part is decompressed, so a
// crafted archive cannot exhaust memory.
const maxDocxBodySize = 64 << 20

// wordprocessingML is the namespace of the elements docx text lives in.
const wordprocessingML = "http://schemas.openxmlformats.org/wordprocessingml/2006/main"

// extractDocxText returns the text of a .docx body, one line per paragraph.
func extractDocxText(path string) (string, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return "", fmt.Errorf("docx: open %s: %w", path, err)
	}
	defer func() { _ = zr.Close() }()

	body, err := zr.Open(docxBodyPart)
	if err != nil {
		return "", fmt.Errorf("docx: %s: open %s: %w", path, docxBodyPart, err)
	}
	defer func() { _ = body.Close() }()
	return docxBodyText(io.LimitReader(body, maxDocxBodySize))
}

// docxBodyText walks WordprocessingML: run text (w:t) joins within a
// paragraph, tabs and breaks keep their whitespace, and each paragraph
// (w:p) ends a line. Empty paragraphs are dropped.
func docxBodyText(r io.Reader) (string, error) {
	dec := xml.NewDecoder(r)
	var (
		lines []string
		para  strings.Builder
		inT   bool
	)
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", fmt.Errorf("docx: parse %s: %w", docxBodyPart, err)
		}
		switch el := tok.(type) {
		case xml.StartElement:
			if el.Name.Space != wordprocessingML {
				continue
			}
			switch el.Name.Local {
			case "t":
				inT = true
			case "tab":
				para.WriteByte('\t')
			case "br", "cr":
				para.WriteByte('\n')
			}
		case xml.EndElement:
			if el.Name.Space != wordprocessingML {
				continue
			}
			switch el.Name.Local {
			case "t":
				inT = false
			case "p":
				if line := strings.TrimSpace(para.String()); line != "" {
					lines = append(lines, line)
				}
				para.Reset()
			}
		case xml.CharData:
			if inT {
				para.Write(el)
			}
		}
	}
	return strings.Join(lines, "\n"), nil
}
