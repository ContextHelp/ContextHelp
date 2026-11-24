package providers

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// defaultSceneThreshold is ffmpeg's scene-change score above which a frame
// counts as a cut, when FrameOptions leaves it unset.
const defaultSceneThreshold = 0.3

// FFmpegFrameProvider selects frames with ffmpeg scene detection bounded
// by the rate options. It is the interim local backend of the frames role.
type FFmpegFrameProvider struct{}

func NewFFmpegFrameProvider() *FFmpegFrameProvider { return &FFmpegFrameProvider{} }

func (p *FFmpegFrameProvider) Name() string { return backendFFmpeg }

var showinfoPTS = regexp.MustCompile(`pts_time:([0-9]+(?:\.[0-9]+)?)`)

// ffmpegInput names a local file for -i or ffprobe: the file: prefix
// keeps ffmpeg from reading a ':' in the name as a protocol or a URL.
func ffmpegInput(path string) string { return "file:" + path }

func (p *FFmpegFrameProvider) ExtractFrames(ctx context.Context, src MediaSource, opts FrameOptions, yield FrameYield) error {
	if err := opts.Validate(); err != nil {
		return err
	}
	if src == nil {
		return errNilMediaSource
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	for _, tool := range []string{"ffprobe", toolFFmpeg} {
		if _, err := LookupTool(tool); err != nil {
			return err
		}
	}
	path, cleanup, err := spoolToFile(ctx, src)
	if err != nil {
		return err
	}
	defer cleanup()

	durationMS, err := probeVideoDurationMS(ctx, path)
	if err != nil {
		return err
	}
	if durationMS == 0 {
		return nil // no video stream: no frames
	}
	var cuts []int64
	if opts.SceneDetection {
		if cuts, err = sceneCutsMS(ctx, path, opts.SceneThreshold, durationMS); err != nil {
			return err
		}
	}
	return emitFrames(ctx, planFrameTimes(durationMS, cuts, opts), func(ms int64) ([]byte, error) {
		args := []string{"-v", "error", "-ss", formatDuration(msToDuration(ms)), "-i", ffmpegInput(path), "-frames:v", "1"}
		if n := opts.MaxDimension; n > 0 {
			// Shrink only; keep the aspect ratio. Sides are not forced
			// even, so a 1px source stays 1px.
			args = append(args, "-vf", fmt.Sprintf(`scale=w=min(iw\,%d):h=min(ih\,%d):force_original_aspect_ratio=decrease`, n, n))
		}
		args = append(args, "-f", "image2pipe", "-vcodec", "png", "pipe:1")
		res, err := RunCommandRaw(ctx, toolFFmpeg, args...)
		if err != nil {
			return nil, fmt.Errorf("ffmpeg extract: %w", err)
		}
		return res.Stdout, nil
	}, "image/png", yield)
}

// probeVideoDurationMS returns the duration of the first video stream that
// is not attached art (cover images), falling back to the container's when
// the stream has none. 0 means the file has no such stream. Durations that
// are not finite or overflow are ErrUnusableMedia.
func probeVideoDurationMS(ctx context.Context, path string) (int64, error) {
	res, err := RunCommand(ctx, "ffprobe", "-v", "error", "-select_streams", "v",
		"-show_entries", "stream=duration:stream_disposition=attached_pic:format=duration",
		"-of", "json", ffmpegInput(path))
	if err != nil {
		return 0, fmt.Errorf("ffprobe: %w", err)
	}
	var out struct {
		Streams []struct {
			Duration    string         `json:"duration"`
			Disposition map[string]int `json:"disposition"`
		} `json:"streams"`
		Format struct {
			Duration string `json:"duration"`
		} `json:"format"`
	}
	if err := json.Unmarshal([]byte(res.Stdout), &out); err != nil {
		return 0, fmt.Errorf("ffprobe: parse output: %w", err)
	}
	streamDuration, found := "", false
	for _, st := range out.Streams {
		if st.Disposition["attached_pic"] == 0 {
			streamDuration, found = st.Duration, true
			break
		}
	}
	if !found {
		return 0, nil
	}
	secs, err := strconv.ParseFloat(streamDuration, 64)
	if err != nil || !finite(secs) || secs <= 0 {
		secs, err = strconv.ParseFloat(out.Format.Duration, 64)
	}
	if err != nil || !finite(secs) || secs <= 0 {
		return 0, fmt.Errorf("%w: ffprobe reports no usable duration", ErrUnusableMedia)
	}
	if secs*1000 > maxVideoDurationMS {
		return 0, fmt.Errorf("%w: ffprobe reports an implausible duration of %gs", ErrUnusableMedia, secs)
	}
	return int64(secs * 1000), nil
}

// sceneCutCap bounds the cuts one scene pass reads; a variable so tests
// can lower it.
var sceneCutCap = planCandidateCap

// sceneCutsMS lists scene cuts in the first durationMS of the video. It
// reads ffmpeg's stderr as a stream and stops ffmpeg once sceneCutCap cuts
// are in, so neither memory nor run time grows with a flickering video.
func sceneCutsMS(ctx context.Context, path string, threshold float64, durationMS int64) ([]int64, error) {
	if threshold == 0 {
		threshold = defaultSceneThreshold
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	cmd := exec.CommandContext(ctx, toolFFmpeg, // #nosec G204 -- fixed tool name, args built here
		"-hide_banner", "-nostats",
		"-t", fmt.Sprintf("%.3f", float64(durationMS)/1000),
		"-i", ffmpegInput(path),
		"-an",
		"-vf", fmt.Sprintf("select='gt(scene,%g)',showinfo", threshold),
		"-f", "null", "-",
	)
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("ffmpeg scene detection: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("ffmpeg scene detection: %w", err)
	}
	cuts, tail, capped, readErr := readSceneCuts(stderr)
	if readErr != nil {
		slog.Warn("providers: ffmpeg scene output unreadable; using the cuts read so far", "err", readErr)
	}
	if capped || readErr != nil {
		cancel() // enough cuts, or output we cannot read: stop ffmpeg
	}
	waitErr := cmd.Wait()
	switch {
	case capped || readErr != nil:
		return cuts, nil
	case waitErr != nil:
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("ffmpeg scene detection: %w: %s", waitErr, strings.Join(tail, "; "))
	}
	return cuts, nil
}

// readSceneCuts reads ffmpeg's showinfo lines from r until EOF, a read
// error, or sceneCutCap cuts. It returns the cuts in milliseconds, the last
// few other lines (the tail an error message quotes), whether the cap
// stopped it, and a read error other than EOF.
func readSceneCuts(r io.Reader) (cuts []int64, tail []string, capped bool, readErr error) {
	rd := bufio.NewReaderSize(r, 64<<10)
	for {
		line, isPrefix, err := rd.ReadLine()
		overlong := isPrefix
		for isPrefix && err == nil {
			// An overlong line is never a showinfo line: skip the rest of
			// it and keep reading.
			_, isPrefix, err = rd.ReadLine()
		}
		if err != nil {
			if !errors.Is(err, io.EOF) {
				readErr = err
			}
			return cuts, tail, false, readErr
		}
		if overlong {
			continue
		}
		text := string(line)
		if m := showinfoPTS.FindStringSubmatch(text); m != nil {
			if secs, err := strconv.ParseFloat(m[1], 64); err == nil && finite(secs) {
				cuts = append(cuts, int64(secs*1000))
				if len(cuts) >= sceneCutCap {
					return cuts, tail, true, nil
				}
			}
			continue
		}
		if tail = append(tail, text); len(tail) > 5 {
			tail = tail[1:]
		}
	}
}

func msToDuration(ms int64) time.Duration { return time.Duration(ms) * time.Millisecond }
