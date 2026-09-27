// Package probe reads ffprobe's JSON output into a typed description of an
// input file's video and audio streams.
package probe

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os/exec"
	"strconv"
	"strings"
)

// ErrNoVideo is returned by Parse when the input has no usable video stream.
var ErrNoVideo = errors.New("no video stream")

// Info describes one input file as reported by ffprobe.
type Info struct {
	Path      string
	Duration  float64 // seconds; 0 if unknown
	SizeBytes int64   // format.size
	BitRate   int64   // format.bit_rate, bps; 0 if unknown
	Format    string  // format.format_name
	Video     VideoStream
	Audio     *AudioStream // nil when the input has no audio
}

// VideoStream describes the chosen video stream, in display orientation.
type VideoStream struct {
	Codec         string // codec_name: "h264", "hevc", "vp9", "av1", ...
	Width, Height int    // display orientation
	FPS           float64
	PixFmt        string
}

// AudioStream describes the first audio stream, if any.
type AudioStream struct {
	Codec      string
	BitRate    int64 // bps; 0 if unknown
	Channels   int
	SampleRate int
}

// ffprobe's raw JSON schema (only the fields lazyff uses).
type ffprobeOutput struct {
	Streams []ffprobeStream `json:"streams"`
	Format  ffprobeFormat   `json:"format"`
}

type ffprobeStream struct {
	CodecName    string            `json:"codec_name"`
	CodecType    string            `json:"codec_type"`
	Width        int               `json:"width"`
	Height       int               `json:"height"`
	PixFmt       string            `json:"pix_fmt"`
	RFrameRate   string            `json:"r_frame_rate"`
	AvgFrameRate string            `json:"avg_frame_rate"`
	Duration     string            `json:"duration"`
	BitRate      string            `json:"bit_rate"`
	SampleRate   string            `json:"sample_rate"`
	Channels     int               `json:"channels"`
	SideDataList []ffprobeSideData `json:"side_data_list"`
	Tags         map[string]string `json:"tags"`
	Disposition  map[string]int    `json:"disposition"`
}

type ffprobeSideData struct {
	SideDataType string  `json:"side_data_type"`
	Rotation     float64 `json:"rotation"`
}

type ffprobeFormat struct {
	FormatName string `json:"format_name"`
	Duration   string `json:"duration"`
	Size       string `json:"size"`
	BitRate    string `json:"bit_rate"`
}

// Parse decodes ffprobe's `-show_format -show_streams` JSON output.
func Parse(data []byte) (Info, error) {
	var raw ffprobeOutput
	if err := json.Unmarshal(data, &raw); err != nil {
		return Info{}, fmt.Errorf("parse ffprobe output: %w", err)
	}

	info := Info{Format: raw.Format.FormatName}
	if v, err := strconv.ParseInt(raw.Format.Size, 10, 64); err == nil {
		info.SizeBytes = v
	}
	if v, err := strconv.ParseInt(raw.Format.BitRate, 10, 64); err == nil {
		info.BitRate = v
	}
	if v, err := strconv.ParseFloat(raw.Format.Duration, 64); err == nil {
		info.Duration = v
	}

	var video *ffprobeStream
	var audio *ffprobeStream
	for i := range raw.Streams {
		st := &raw.Streams[i]
		switch st.CodecType {
		case "video":
			if video == nil && st.Disposition["attached_pic"] == 0 {
				video = st
			}
		case "audio":
			if audio == nil {
				audio = st
			}
		}
	}
	if video == nil {
		return Info{}, ErrNoVideo
	}

	width, height := video.Width, video.Height
	if r := normalizeRotation(rotationOf(video)); r == 90 || r == 270 {
		width, height = height, width
	}

	fps := parseFrameRate(video.AvgFrameRate)
	if fps == 0 {
		fps = parseFrameRate(video.RFrameRate)
	}

	info.Video = VideoStream{
		Codec:  video.CodecName,
		Width:  width,
		Height: height,
		FPS:    fps,
		PixFmt: video.PixFmt,
	}

	if info.Duration == 0 {
		if v, err := strconv.ParseFloat(video.Duration, 64); err == nil {
			info.Duration = v
		}
	}

	if audio != nil {
		a := AudioStream{Codec: audio.CodecName, Channels: audio.Channels}
		if v, err := strconv.ParseInt(audio.BitRate, 10, 64); err == nil {
			a.BitRate = v
		}
		if v, err := strconv.Atoi(audio.SampleRate); err == nil {
			a.SampleRate = v
		}
		info.Audio = &a
	}

	return info, nil
}

func rotationOf(st *ffprobeStream) float64 {
	for _, sd := range st.SideDataList {
		if sd.SideDataType == "Display Matrix" {
			return sd.Rotation
		}
	}
	if r, ok := st.Tags["rotate"]; ok {
		if v, err := strconv.ParseFloat(r, 64); err == nil {
			return v
		}
	}
	return 0
}

func normalizeRotation(r float64) int {
	n := int(math.Round(r)) % 360
	if n < 0 {
		n += 360
	}
	return n
}

func parseFrameRate(s string) float64 {
	parts := strings.SplitN(s, "/", 2)
	if len(parts) != 2 {
		return 0
	}
	num, err1 := strconv.ParseFloat(parts[0], 64)
	den, err2 := strconv.ParseFloat(parts[1], 64)
	if err1 != nil || err2 != nil || den == 0 {
		return 0
	}
	return num / den
}

// Run execs `ffprobe -v error -print_format json -show_format -show_streams
// <path>` and parses its output.
func Run(ctx context.Context, path string) (Info, error) {
	cmd := exec.CommandContext(ctx, "ffprobe", "-v", "error", "-print_format", "json", "-show_format", "-show_streams", path)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return Info{}, fmt.Errorf("ffprobe: %v: %s", err, strings.TrimSpace(stderr.String()))
	}
	info, err := Parse(stdout.Bytes())
	if err != nil {
		return Info{}, err
	}
	info.Path = path
	return info, nil
}
