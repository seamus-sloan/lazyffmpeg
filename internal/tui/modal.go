package tui

import (
	"fmt"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/seamus-sloan/lazyffmpeg/internal/pipeline"
	"github.com/seamus-sloan/lazyffmpeg/internal/units"
)

// modalOption is one row in a step modal: a concrete preset step, or
// Custom… (step == nil) whose text is parsed by the kind's Parse* function.
type modalOption struct {
	label string
	step  pipeline.Step
}

// modalState is the step modal's state while Model.modal != nil.
type modalState struct {
	kind    pipeline.Kind
	options []modalOption
	cursor  int
	input   textinput.Model
	err     string
}

func isCustomOption(opts []modalOption, i int) bool {
	return i >= 0 && i < len(opts) && opts[i].step == nil
}

// presetsFor returns kind's preset options against p (Quality's presets
// depend on codec, the pipeline's effective encoder).
func presetsFor(kind pipeline.Kind, p pipeline.Pipeline, codec pipeline.Codec) []modalOption {
	switch kind {
	case pipeline.KindResolution:
		return []modalOption{
			{"3840×2160", pipeline.Resolution{Width: 3840, Height: 2160}},
			{"2560×1440", pipeline.Resolution{Width: 2560, Height: 1440}},
			{"1920×1080", pipeline.Resolution{Width: 1920, Height: 1080}},
			{"1280×720", pipeline.Resolution{Width: 1280, Height: 720}},
			{"854×480", pipeline.Resolution{Width: 854, Height: 480}},
			{"50%", pipeline.Resolution{Percent: 50}},
			{"Custom…", nil},
		}
	case pipeline.KindSpeed:
		return []modalOption{
			{"0.5x", pipeline.Speed{Factor: 0.5}},
			{"1.5x", pipeline.Speed{Factor: 1.5}},
			{"2x", pipeline.Speed{Factor: 2}},
			{"3x", pipeline.Speed{Factor: 3}},
			{"4x", pipeline.Speed{Factor: 4}},
			{"Custom…", nil},
		}
	case pipeline.KindTrim:
		return []modalOption{{"Custom…", nil}}
	case pipeline.KindFrameRate:
		return []modalOption{
			{"60", pipeline.FrameRate{FPS: 60}},
			{"30", pipeline.FrameRate{FPS: 30}},
			{"24", pipeline.FrameRate{FPS: 24}},
			{"15", pipeline.FrameRate{FPS: 15}},
			{"10", pipeline.FrameRate{FPS: 10}},
			{"Custom…", nil},
		}
	case pipeline.KindEncoder:
		return []modalOption{
			{"h264", pipeline.Encoder{Codec: pipeline.CodecH264}},
			{"h265", pipeline.Encoder{Codec: pipeline.CodecH265}},
			{"av1", pipeline.Encoder{Codec: pipeline.CodecAV1}},
			{"vp9", pipeline.Encoder{Codec: pipeline.CodecVP9}},
			{"h264 hardware", pipeline.Encoder{Codec: pipeline.CodecH264HW}},
			{"h265 hardware", pipeline.Encoder{Codec: pipeline.CodecH265HW}},
			{"copy", pipeline.Encoder{Codec: pipeline.CodecCopy}},
		}
	case pipeline.KindQuality:
		if codec == pipeline.CodecCopy {
			return nil
		}
		d := pipeline.DefaultCRF(codec)
		return []modalOption{
			{fmt.Sprintf("Default (CRF %d)", d), pipeline.Quality{CRF: d}},
			{fmt.Sprintf("Higher quality (CRF %d)", d-5), pipeline.Quality{CRF: d - 5}},
			{fmt.Sprintf("Smaller (CRF %d)", d+5), pipeline.Quality{CRF: d + 5}},
			{"Target 10 MB", pipeline.Quality{TargetBytes: 10_000_000}},
			{"Target 25 MB", pipeline.Quality{TargetBytes: 25_000_000}},
			{"Target 50 MB", pipeline.Quality{TargetBytes: 50_000_000}},
			{"Custom…", nil},
		}
	case pipeline.KindAudio:
		return []modalOption{
			{"Keep", pipeline.Audio{Mode: pipeline.AudioKeep}},
			{"Remove", pipeline.Audio{Mode: pipeline.AudioRemove}},
			{"AAC 96k", pipeline.Audio{Mode: pipeline.AudioAAC, BitrateK: 96}},
			{"AAC 128k", pipeline.Audio{Mode: pipeline.AudioAAC, BitrateK: 128}},
			{"AAC 192k", pipeline.Audio{Mode: pipeline.AudioAAC, BitrateK: 192}},
			{"Custom…", nil},
		}
	case pipeline.KindContainer:
		return []modalOption{
			{"mp4", pipeline.Container{Format: pipeline.FormatMP4}},
			{"mov", pipeline.Container{Format: pipeline.FormatMOV}},
			{"mkv", pipeline.Container{Format: pipeline.FormatMKV}},
			{"webm", pipeline.Container{Format: pipeline.FormatWebM}},
		}
	case pipeline.KindRawArgs:
		return []modalOption{{"Custom…", nil}}
	}
	return nil
}

// placeholderFor is the Custom… text input's placeholder hint for kind.
func placeholderFor(kind pipeline.Kind) string {
	switch kind {
	case pipeline.KindResolution:
		return "WxH (fit), WxH! (stretch), Wx, xH, 50%"
	case pipeline.KindTrim:
		return "0:05-0:20"
	}
	return ""
}

// customPrefill renders s back into the text a user would type to produce
// it via the kind's Parse* function, for prefilling Custom… when reopening
// a step's modal.
func customPrefill(s pipeline.Step) string {
	switch v := s.(type) {
	case pipeline.Resolution:
		switch {
		case v.Percent > 0:
			return units.FormatNumber(v.Percent) + "%"
		case v.Width > 0 && v.Height > 0:
			suffix := ""
			if v.Exact {
				suffix = "!"
			}
			return fmt.Sprintf("%dx%d%s", v.Width, v.Height, suffix)
		case v.Width > 0:
			return fmt.Sprintf("%dx", v.Width)
		case v.Height > 0:
			return fmt.Sprintf("x%d", v.Height)
		}
	case pipeline.Speed:
		return units.FormatNumber(v.Factor) + "x"
	case pipeline.Trim:
		start, end := "", ""
		if v.Start > 0 {
			start = units.FormatNumber(v.Start)
		}
		if v.End > 0 {
			end = units.FormatNumber(v.End)
		}
		return start + "-" + end
	case pipeline.FrameRate:
		return units.FormatNumber(v.FPS)
	case pipeline.Quality:
		if v.TargetBytes > 0 {
			return units.FormatSize(v.TargetBytes)
		}
		return strconv.Itoa(v.CRF)
	case pipeline.Audio:
		switch v.Mode {
		case pipeline.AudioKeep:
			return "keep"
		case pipeline.AudioRemove:
			return "remove"
		case pipeline.AudioAAC:
			return fmt.Sprintf("aac:%dk", v.BitrateK)
		}
	case pipeline.RawArgs:
		return v.Text
	}
	return ""
}

// parseCustom parses text as kind's custom value.
func parseCustom(kind pipeline.Kind, text string) (pipeline.Step, error) {
	switch kind {
	case pipeline.KindResolution:
		v, err := pipeline.ParseResolution(text)
		if err != nil {
			return nil, err
		}
		return v, nil
	case pipeline.KindSpeed:
		v, err := pipeline.ParseSpeed(text)
		if err != nil {
			return nil, err
		}
		return v, nil
	case pipeline.KindTrim:
		v, err := pipeline.ParseTrim(text)
		if err != nil {
			return nil, err
		}
		return v, nil
	case pipeline.KindFrameRate:
		v, err := pipeline.ParseFPS(text)
		if err != nil {
			return nil, err
		}
		return v, nil
	case pipeline.KindQuality:
		v, err := pipeline.ParseQuality(text)
		if err != nil {
			return nil, err
		}
		return v, nil
	case pipeline.KindAudio:
		v, err := pipeline.ParseAudio(text)
		if err != nil {
			return nil, err
		}
		return v, nil
	case pipeline.KindRawArgs:
		args, err := pipeline.SplitArgs(text)
		if err != nil {
			return nil, err
		}
		return pipeline.RawArgs{Text: text, Args: args}, nil
	}
	return nil, fmt.Errorf("no custom parser for %v", kind)
}

// openModal opens kind's step modal, preselecting the pipeline's current
// value for kind when there is one (a matching preset, or Custom…
// prefilled with its text).
func (m Model) openModal(kind pipeline.Kind) Model {
	container := pipeline.EffectiveContainer(m.session.OutputPath(m.pipeline), m.pipeline)
	codec := pipeline.EffectiveCodec(m.pipeline, container)

	if kind == pipeline.KindQuality && codec == pipeline.CodecCopy {
		m.notice = "quality doesn't apply to copy"
		return m
	}

	opts := presetsFor(kind, m.pipeline, codec)
	ms := modalState{kind: kind, options: opts}

	ti := textinput.New()
	ti.Placeholder = placeholderFor(kind)
	ti.SetWidth(40)

	current, hasCurrent := m.pipeline.Find(kind)
	if hasCurrent {
		matched := false
		for i, o := range opts {
			if o.step != nil && o.step == current {
				ms.cursor = i
				matched = true
				break
			}
		}
		if !matched {
			for i, o := range opts {
				if o.step == nil {
					ms.cursor = i
					ti.SetValue(customPrefill(current))
					ti.CursorEnd()
					break
				}
			}
		}
	}
	if isCustomOption(opts, ms.cursor) {
		ti.Focus()
	}
	ms.input = ti

	m.modal = &ms
	return m
}

func (m Model) openModalForCurrentStep() Model {
	steps := m.pipeline.Steps()
	if m.pipelineCursor < 0 || m.pipelineCursor >= len(steps) {
		return m
	}
	return m.openModal(steps[m.pipelineCursor].Kind())
}

func (m Model) moveModalCursor(delta int) Model {
	if m.modal == nil || len(m.modal.options) == 0 {
		return m
	}
	ms := *m.modal
	ms.cursor = clamp(ms.cursor+delta, 0, len(ms.options)-1)
	if isCustomOption(ms.options, ms.cursor) {
		ms.input.Focus()
	} else {
		ms.input.Blur()
	}
	m.modal = &ms
	return m
}

func (m Model) confirmModal() Model {
	ms := *m.modal
	if len(ms.options) == 0 {
		m.modal = nil
		return m
	}
	var step pipeline.Step
	if isCustomOption(ms.options, ms.cursor) {
		parsed, err := parseCustom(ms.kind, ms.input.Value())
		if err != nil {
			ms.err = err.Error()
			m.modal = &ms
			return m
		}
		step = parsed
	} else {
		step = ms.options[ms.cursor].step
	}
	m = m.pushUndo()
	m.pipeline = m.pipeline.Upsert(step)
	m.modal = nil
	return m
}

func (m Model) handleModalKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	k := msg.String()

	switch k {
	case "ctrl+c":
		m.quitting = true
		return m, tea.Quit
	case "esc":
		m.modal = nil
		return m, nil
	case "up":
		m = m.moveModalCursor(-1)
		return m, nil
	case "down":
		m = m.moveModalCursor(1)
		return m, nil
	case "enter":
		m = m.confirmModal()
		if m.modal == nil {
			return m.maybeRerenderResult()
		}
		return m, nil
	}

	if !isCustomOption(m.modal.options, m.modal.cursor) {
		switch k {
		case "j":
			m = m.moveModalCursor(1)
		case "k":
			m = m.moveModalCursor(-1)
		}
		return m, nil
	}

	ms := *m.modal
	input, cmd := ms.input.Update(msg)
	ms.input = input
	m.modal = &ms
	return m, cmd
}

func (m Model) modalView() string {
	ms := m.modal
	var b strings.Builder
	b.WriteString(ms.kind.Label())
	b.WriteString("\n\n")
	for i, o := range ms.options {
		prefix := "  "
		if i == ms.cursor {
			prefix = "> "
		}
		if o.step == nil {
			b.WriteString(prefix + "Custom…\n")
			b.WriteString("    " + ms.input.View() + "\n")
		} else {
			b.WriteString(prefix + o.label + "\n")
		}
	}
	if ms.err != "" {
		b.WriteString("\n" + errorStyle.Render(ms.err) + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func (m Model) overlayModal(base string) string {
	modal := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		Padding(1, 2).
		Render(m.modalView())

	baseW, baseH := lipgloss.Width(base), lipgloss.Height(base)
	mw, mh := lipgloss.Width(modal), lipgloss.Height(modal)
	x := maxInt((baseW-mw)/2, 0)
	y := maxInt((baseH-mh)/2, 0)

	c := lipgloss.NewCompositor(
		lipgloss.NewLayer(base),
		lipgloss.NewLayer(modal).X(x).Y(y).Z(1),
	)
	return c.Render()
}
