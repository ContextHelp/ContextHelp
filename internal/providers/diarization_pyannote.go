package providers

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// PyannoteDiarizationProvider shells out to pyannote CLI.
type PyannoteDiarizationProvider struct {
	cmd  string
	args []string
}

func NewPyannoteDiarizationProvider(cmd string, args ...string) *PyannoteDiarizationProvider {
	return &PyannoteDiarizationProvider{
		cmd:  cmd,
		args: args,
	}
}

func (p *PyannoteDiarizationProvider) Name() string { return "pyannote" }

func (p *PyannoteDiarizationProvider) Diarize(ctx context.Context, audioPath string, segments []TranscriptSegment) (*DiarizedResult, error) {
	// Run pyannote-audio CLI which outputs RTTM format.
	args := append([]string{}, p.args...)
	args = append(args, "--input", audioPath)

	result, err := RunCommand(ctx, p.cmd, args...)
	if err != nil {
		return nil, fmt.Errorf("pyannote: %w", err)
	}

	// Parse RTTM output.
	speakerRegions := parseRTTM(result.Stdout)

	// Attribute each transcript segment to the speaker with most overlap.
	speakers := make(map[string]bool)
	attributed := make([]TranscriptSegment, len(segments))
	copy(attributed, segments)

	for i, seg := range attributed {
		bestSpeaker := ""
		bestOverlap := time.Duration(0)
		for _, region := range speakerRegions {
			overlap := computeOverlap(seg.StartTime, seg.EndTime, region.start, region.end)
			if overlap > bestOverlap {
				bestOverlap = overlap
				bestSpeaker = region.speaker
			}
		}
		if bestSpeaker != "" {
			attributed[i].Speaker = bestSpeaker
			speakers[bestSpeaker] = true
		}
	}

	labels := make([]string, 0, len(speakers))
	for s := range speakers {
		labels = append(labels, s)
	}
	sort.Strings(labels)

	return &DiarizedResult{
		SpeakerCount:  len(labels),
		SpeakerLabels: labels,
		Segments:      attributed,
	}, nil
}

type speakerRegion struct {
	speaker string
	start   time.Duration
	end     time.Duration
}

// parseRTTM parses RTTM (Rich Transcription Time Marked) format.
// Format: SPEAKER <file> <channel> <start> <dur> <na> <na> <speaker> <na> <na>
func parseRTTM(output string) []speakerRegion {
	var regions []speakerRegion
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 8 || fields[0] != "SPEAKER" {
			continue
		}
		start, err := strconv.ParseFloat(fields[3], 64)
		if err != nil {
			continue
		}
		dur, err := strconv.ParseFloat(fields[4], 64)
		if err != nil {
			continue
		}
		speaker := fields[7]

		regions = append(regions, speakerRegion{
			speaker: speaker,
			start:   time.Duration(start * float64(time.Second)),
			end:     time.Duration((start + dur) * float64(time.Second)),
		})
	}
	return regions
}

func computeOverlap(aStart, aEnd, bStart, bEnd time.Duration) time.Duration {
	start := aStart
	if bStart > start {
		start = bStart
	}
	end := aEnd
	if bEnd < end {
		end = bEnd
	}
	if end > start {
		return end - start
	}
	return 0
}
