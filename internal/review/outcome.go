package review

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"path"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Verdict is the review gate decision.
type Verdict string

const (
	VerdictApprove Verdict = "approve"
	VerdictFix     Verdict = "fix"
)

// String returns the serialized verdict value.
func (v Verdict) String() string {
	return string(v)
}

// Severity classifies the effect of a review finding.
type Severity string

const (
	SeverityBlocking    Severity = "blocking"
	SeverityNonBlocking Severity = "non-blocking"
	SeverityNit         Severity = "nit"
)

// String returns the serialized severity value.
func (s Severity) String() string {
	return string(s)
}

// Finding is one validated review observation.
type Finding struct {
	Severity    Severity
	File        *string
	Line        *int
	Description string
}

// Outcome is one complete, validated review result.
type Outcome struct {
	Verdict  Verdict
	Findings []Finding
}

// ParseOutcome parses and validates the reviewer's complete final output.
func ParseOutcome(output string) (Outcome, error) {
	trimmed := strings.TrimSpace(output)
	if trimmed == "" {
		return Outcome{}, fmt.Errorf("parse review outcome: output is empty")
	}
	if !utf8.ValidString(trimmed) {
		return Outcome{}, fmt.Errorf("parse review outcome: output is not valid UTF-8")
	}
	if err := validateSingleJSONValue(trimmed); err != nil {
		return Outcome{}, fmt.Errorf("parse review outcome: %w", err)
	}

	document, err := decodeObject([]byte(trimmed), "document")
	if err != nil {
		return Outcome{}, fmt.Errorf("parse review outcome: %w", err)
	}
	if err := requireExactKeys(document, "document", "verdict", "findings"); err != nil {
		return Outcome{}, fmt.Errorf("parse review outcome: %w", err)
	}

	verdict, err := parseVerdict(document["verdict"])
	if err != nil {
		return Outcome{}, fmt.Errorf("parse review outcome: %w", err)
	}
	findings, err := parseFindings(document["findings"])
	if err != nil {
		return Outcome{}, fmt.Errorf("parse review outcome: %w", err)
	}
	if err := validateOutcomeSemantics(verdict, findings); err != nil {
		return Outcome{}, fmt.Errorf("parse review outcome: %w", err)
	}

	return Outcome{Verdict: verdict, Findings: findings}, nil
}

func validateSingleJSONValue(output string) error {
	decoder := json.NewDecoder(strings.NewReader(output))
	decoder.UseNumber()
	if err := consumeJSONValue(decoder, "document"); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}
	if _, err := decoder.Token(); err == nil {
		return fmt.Errorf("trailing JSON value")
	} else if err != io.EOF {
		return fmt.Errorf("trailing content after JSON document: %w", err)
	}
	return nil
}

func consumeJSONValue(decoder *json.Decoder, location string) error {
	token, err := decoder.Token()
	if err != nil {
		return fmt.Errorf("read %s: %w", location, err)
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}

	switch delimiter {
	case '{':
		seen := make(map[string]struct{})
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return fmt.Errorf("read key in %s: %w", location, err)
			}
			key, ok := keyToken.(string)
			if !ok {
				return fmt.Errorf("read key in %s: expected string", location)
			}
			if _, exists := seen[key]; exists {
				return fmt.Errorf("duplicate key %q in %s", key, location)
			}
			seen[key] = struct{}{}
			if err := consumeJSONValue(decoder, location+"."+key); err != nil {
				return err
			}
		}
		if _, err := decoder.Token(); err != nil {
			return fmt.Errorf("close object %s: %w", location, err)
		}
	case '[':
		for index := 0; decoder.More(); index++ {
			if err := consumeJSONValue(decoder, fmt.Sprintf("%s[%d]", location, index)); err != nil {
				return err
			}
		}
		if _, err := decoder.Token(); err != nil {
			return fmt.Errorf("close array %s: %w", location, err)
		}
	default:
		return fmt.Errorf("unexpected delimiter %q in %s", delimiter, location)
	}
	return nil
}

func decodeObject(raw []byte, location string) (map[string]json.RawMessage, error) {
	if firstNonSpace(raw) != '{' {
		return nil, fmt.Errorf("%s must be a JSON object", location)
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil {
		return nil, fmt.Errorf("decode %s object: %w", location, err)
	}
	return object, nil
}

func requireExactKeys(object map[string]json.RawMessage, location string, required ...string) error {
	allowed := make(map[string]struct{}, len(required))
	for _, key := range required {
		allowed[key] = struct{}{}
		if _, ok := object[key]; !ok {
			return fmt.Errorf("%s is missing required key %q", location, key)
		}
	}

	unknown := make([]string, 0)
	for key := range object {
		if _, ok := allowed[key]; !ok {
			unknown = append(unknown, key)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return fmt.Errorf("%s contains unknown key %q", location, unknown[0])
	}
	return nil
}

func parseVerdict(raw json.RawMessage) (Verdict, error) {
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", fmt.Errorf("verdict must be a string: %w", err)
	}
	switch Verdict(value) {
	case VerdictApprove, VerdictFix:
		return Verdict(value), nil
	default:
		return "", fmt.Errorf("verdict %q is invalid", value)
	}
}

func parseFindings(raw json.RawMessage) ([]Finding, error) {
	if firstNonSpace(raw) != '[' {
		return nil, fmt.Errorf("findings must be a JSON array")
	}
	var documents []json.RawMessage
	if err := json.Unmarshal(raw, &documents); err != nil {
		return nil, fmt.Errorf("decode findings array: %w", err)
	}

	findings := make([]Finding, 0, len(documents))
	for index, document := range documents {
		finding, err := parseFinding(document, index)
		if err != nil {
			return nil, err
		}
		findings = append(findings, finding)
	}
	return findings, nil
}

func parseFinding(raw json.RawMessage, index int) (Finding, error) {
	location := fmt.Sprintf("findings[%d]", index)
	object, err := decodeObject(raw, location)
	if err != nil {
		return Finding{}, err
	}
	if err := requireExactKeys(object, location, "severity", "file", "line", "description"); err != nil {
		return Finding{}, err
	}

	severity, err := parseSeverity(object["severity"], location)
	if err != nil {
		return Finding{}, err
	}
	file, err := parseFile(object["file"], location)
	if err != nil {
		return Finding{}, err
	}
	line, err := parseLine(object["line"], location)
	if err != nil {
		return Finding{}, err
	}
	if line != nil && file == nil {
		return Finding{}, fmt.Errorf("%s file must be non-null when line is non-null", location)
	}
	description, err := parseDescription(object["description"], location)
	if err != nil {
		return Finding{}, err
	}

	return Finding{Severity: severity, File: file, Line: line, Description: description}, nil
}

func parseSeverity(raw json.RawMessage, location string) (Severity, error) {
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", fmt.Errorf("%s severity must be a string: %w", location, err)
	}
	switch Severity(value) {
	case SeverityBlocking, SeverityNonBlocking, SeverityNit:
		return Severity(value), nil
	default:
		return "", fmt.Errorf("%s severity %q is invalid", location, value)
	}
}

func parseFile(raw json.RawMessage, location string) (*string, error) {
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, nil
	}
	var file string
	if err := json.Unmarshal(raw, &file); err != nil {
		return nil, fmt.Errorf("%s file must be a string or null: %w", location, err)
	}
	if !isCleanRepositoryPath(file) {
		return nil, fmt.Errorf("%s file %q is not a clean repository-relative path", location, file)
	}
	return &file, nil
}

func isCleanRepositoryPath(file string) bool {
	if file == "" || file == "." || path.IsAbs(file) {
		return false
	}
	if strings.ContainsRune(file, '\x00') || strings.Contains(file, `\`) {
		return false
	}
	if len(file) >= 2 && file[1] == ':' {
		return false
	}
	if path.Clean(file) != file {
		return false
	}
	for _, component := range strings.Split(file, "/") {
		if component == ".." {
			return false
		}
	}
	return true
}

func parseLine(raw json.RawMessage, location string) (*int, error) {
	trimmed := bytes.TrimSpace(raw)
	if bytes.Equal(trimmed, []byte("null")) {
		return nil, nil
	}
	value, err := strconv.ParseInt(string(trimmed), 10, 0)
	if err != nil || value <= 0 {
		return nil, fmt.Errorf("%s line must be a positive integer or null", location)
	}
	line := int(value)
	return &line, nil
}

func parseDescription(raw json.RawMessage, location string) (string, error) {
	var description string
	if err := json.Unmarshal(raw, &description); err != nil {
		return "", fmt.Errorf("%s description must be a string: %w", location, err)
	}
	if strings.TrimSpace(description) == "" {
		return "", fmt.Errorf("%s description must contain non-whitespace text", location)
	}
	return description, nil
}

func validateOutcomeSemantics(verdict Verdict, findings []Finding) error {
	hasBlocking := false
	for _, finding := range findings {
		if finding.Severity == SeverityBlocking {
			hasBlocking = true
			break
		}
	}
	if verdict == VerdictApprove && hasBlocking {
		return fmt.Errorf("approve verdict cannot contain blocking findings")
	}
	if verdict == VerdictFix && !hasBlocking {
		return fmt.Errorf("fix verdict requires at least one blocking finding")
	}
	return nil
}

func firstNonSpace(raw []byte) byte {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return 0
	}
	return trimmed[0]
}
