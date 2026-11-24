package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// FFmpegVideoProvider uses ffprobe and ffmpeg CLI tools.
type FFmpegVideoProvider struct{}

func NewFFmpegVideoProvider() *FFmpegVideoProvider {
	return &FFmpegVideoProvider{}
}

func (p *FFmpegVideoProvider) Name() string { return backendFFmpeg }

// ffprobeOutput maps the JSON output of ffprobe -show_format -show_streams.
type ffprobeOutput struct {
	Streams []ffprobeStream `json:"streams"`
	Format  ffprobeFormat   `json:"format"`
}

type ffprobeStream struct {
	CodecType  string `json:"codec_type"`
	CodecName  string `json:"codec_name"`
	Width      int    `json:"width"`
	Height     int    `json:"height"`
	RFrameRate string `json:"r_frame_rate"`
	Duration   string `json:"duration"`
	SampleRate string `json:"sample_rate"`
}

type ffprobeFormat struct {
	Duration string `json:"duration"`
	Size     string `json:"size"`
}

func (p *FFmpegVideoProvider) ProbeMetadata(ctx context.Context, videoPath string) (*VideoMetadata, error) {
	if err := requireAbsMediaPath(videoPath); err != nil {
		return nil, err
	}
	result, err := RunCommand(ctx, "ffprobe",
		"-v", "quiet",
		"-print_format", "json",
		"-show_format",
		"-show_streams",
		ffmpegInput(videoPath),
	)
	if err != nil {
		return nil, fmt.Errorf("ffprobe: %w", err)
	}

	var probe ffprobeOutput
	if err := json.Unmarshal([]byte(result.Stdout), &probe); err != nil {
		return nil, fmt.Errorf("ffprobe: parse output: %w", err)
	}

	meta := &VideoMetadata{}

	// Parse format-level fields.
	if dur, err := strconv.ParseFloat(probe.Format.Duration, 64); err == nil {
		meta.Duration = time.Duration(dur * float64(time.Second))
	}
	if sz, err := strconv.ParseInt(probe.Format.Size, 10, 64); err == nil {
		meta.FileSize = sz
	}

	// Find video and audio streams.
	for _, s := range probe.Streams {
		switch s.CodecType {
		case "video":
			meta.Width = s.Width
			meta.Height = s.Height
			meta.Codec = s.CodecName
			meta.FPS = parseFrameRate(s.RFrameRate)
		case "audio":
			meta.AudioCodec = s.CodecName
		}
	}

	return meta, nil
}

func (p *FFmpegVideoProvider) ExtractAudio(ctx context.Context, videoPath, outDir string) (*ExtractedAudio, error) {
	if err := requireAbsMediaPath(videoPath); err != nil {
		return nil, err
	}
	outPath := filepath.Join(outDir, filepath.Base(videoPath)+".audio.wav")
	_, err := RunCommand(ctx, toolFFmpeg,
		"-i", ffmpegInput(videoPath),
		"-vn",
		"-acodec", "pcm_s16le",
		"-ar", "16000",
		"-ac", "1",
		"-y",
		outPath,
	)
	if err != nil {
		return nil, fmt.Errorf("ffmpeg extract audio: %w", err)
	}

	return &ExtractedAudio{
		Path:        outPath,
		Format:      "wav",
		SampleRate:  16000,
		ContentType: "audio/wav",
	}, nil
}

// parseFrameRate parses ffprobe's r_frame_rate (e.g. "30000/1001" or "30/1").
func parseFrameRate(rate string) float64 {
	parts := strings.SplitN(rate, "/", 2)
	if len(parts) != 2 {
		f, _ := strconv.ParseFloat(rate, 64)
		return f
	}
	num, _ := strconv.ParseFloat(parts[0], 64)
	den, _ := strconv.ParseFloat(parts[1], 64)
	if den == 0 {
		return 0
	}
	return num / den
}

// formatDuration formats a time.Duration as HH:MM:SS.mmm for ffmpeg.
func formatDuration(d time.Duration) string {
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60
	ms := int(d.Milliseconds()) % 1000
	return fmt.Sprintf("%02d:%02d:%02d.%03d", h, m, s, ms)
}
