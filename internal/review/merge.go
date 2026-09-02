package review

import "strings"

type findingKey struct {
	file        string
	line        int
	description string
}

// MergeOutcomes combines independent lens outcomes in caller order.
func MergeOutcomes(outcomes []Outcome) Outcome {
	merged := Outcome{Verdict: VerdictApprove, Findings: []Finding{}}
	indexes := make(map[findingKey]int)

	for _, outcome := range outcomes {
		for _, finding := range outcome.Findings {
			key := keyForFinding(finding)
			if index, ok := indexes[key]; ok {
				if severityRank(finding.Severity) > severityRank(merged.Findings[index].Severity) {
					merged.Findings[index].Severity = finding.Severity
				}
				continue
			}
			indexes[key] = len(merged.Findings)
			merged.Findings = append(merged.Findings, finding)
		}
	}

	for _, finding := range merged.Findings {
		if finding.Severity == SeverityBlocking {
			merged.Verdict = VerdictFix
			break
		}
	}
	return merged
}

func keyForFinding(finding Finding) findingKey {
	key := findingKey{description: strings.ToLower(strings.Join(strings.Fields(finding.Description), " "))}
	if finding.File != nil {
		key.file = *finding.File
	}
	if finding.Line != nil {
		key.line = *finding.Line
	}
	return key
}

func severityRank(severity Severity) int {
	switch severity {
	case SeverityBlocking:
		return 3
	case SeverityNonBlocking:
		return 2
	case SeverityNit:
		return 1
	default:
		return 0
	}
}
