// Package runner execs ffmpeg with progress reporting, cancellation and an
// atomic replace of the output file.
package runner

import (
	"bufio"
	"io"
	"strconv"
	"strings"
)

// Sample is one ffmpeg -progress update block.
type Sample struct {
	OutTime float64 // seconds of output written so far
	Speed   float64 // × realtime; 0 if unknown
	End     bool    // true on progress=end
}

// ParseProgress reads ffmpeg's `-progress pipe:1` key=value stream from r
// and calls emit once per "progress=" line, with the latest out_time_us
// (converted to seconds) and speed ("N.Nx" -> N.N) seen so far. "N/A"
// values are ignored (the tracked value is left unchanged). Unknown keys
// and blank lines are ignored.
func ParseProgress(r io.Reader, emit func(Sample)) {
	var outTime, speed float64
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		idx := strings.Index(line, "=")
		if idx < 0 {
			continue
		}
		key, val := line[:idx], line[idx+1:]
		switch key {
		case "out_time_us":
			if v, err := strconv.ParseFloat(val, 64); err == nil {
				outTime = v / 1e6
			}
		case "speed":
			v := strings.TrimSuffix(strings.TrimSpace(val), "x")
			if f, err := strconv.ParseFloat(v, 64); err == nil {
				speed = f
			}
		case "progress":
			emit(Sample{OutTime: outTime, Speed: speed, End: val == "end"})
		}
	}
}
