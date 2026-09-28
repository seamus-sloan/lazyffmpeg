package pipeline

import "testing"

func kindsOf(steps []Step) []Kind {
	ks := make([]Kind, len(steps))
	for i, s := range steps {
		ks[i] = s.Kind()
	}
	return ks
}

func kindsEqual(a, b []Kind) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestPipelineUpsertAppendsFilterAfterLast(t *testing.T) {
	p := New(Resolution{Width: 1920, Height: 1080}, Speed{Factor: 2})
	p2 := p.Upsert(Trim{Start: 1, End: 3})
	got := kindsOf(p2.Steps())
	want := []Kind{KindResolution, KindSpeed, KindTrim}
	if !kindsEqual(got, want) {
		t.Errorf("Steps() kinds = %v, want %v", got, want)
	}
}

func TestPipelineUpsertReplacesInPlace(t *testing.T) {
	p := New(Resolution{Width: 1920, Height: 1080}, Speed{Factor: 2}, Trim{Start: 1, End: 3})
	p2 := p.Upsert(Speed{Factor: 4})
	steps := p2.Steps()
	got := kindsOf(steps)
	want := []Kind{KindResolution, KindSpeed, KindTrim}
	if !kindsEqual(got, want) {
		t.Errorf("Steps() kinds = %v, want %v (position kept)", got, want)
	}
	sp, ok := steps[1].(Speed)
	if !ok || sp.Factor != 4 {
		t.Errorf("Steps()[1] = %+v, want Speed{Factor:4}", steps[1])
	}
	if p.Len() != 3 {
		t.Errorf("original Len() = %d, want 3 (no duplicate, receiver unchanged)", p.Len())
	}
}

func TestPipelineOutputsCanonicalOrder(t *testing.T) {
	// Upsert outputs out of canonical order; Steps() must still list them
	// Encoder, Quality, Audio, Container, RawArgs.
	p := New(
		RawArgs{Text: "-an", Args: []string{"-an"}},
		Container{Format: FormatMP4},
		Audio{Mode: AudioKeep},
		Quality{CRF: 23},
		Encoder{Codec: CodecH264},
	)
	got := kindsOf(p.Steps())
	want := []Kind{KindEncoder, KindQuality, KindAudio, KindContainer, KindRawArgs}
	if !kindsEqual(got, want) {
		t.Errorf("Steps() kinds = %v, want %v", got, want)
	}
}

func TestPipelineListsFileNameAfterContainerBeforeRawArgs(t *testing.T) {
	p := New(
		RawArgs{Text: "-an", Args: []string{"-an"}},
		Filename{Name: "demo.mp4"},
		Container{Format: FormatMP4},
		Encoder{Codec: CodecH264},
	)
	got := kindsOf(p.Steps())
	want := []Kind{KindEncoder, KindContainer, KindFilename, KindRawArgs}
	if !kindsEqual(got, want) {
		t.Errorf("Steps() kinds = %v, want %v", got, want)
	}
}

func TestPipelineFiltersBeforeOutputs(t *testing.T) {
	p := New(Encoder{Codec: CodecH264}, Speed{Factor: 2})
	got := kindsOf(p.Steps())
	want := []Kind{KindSpeed, KindEncoder}
	if !kindsEqual(got, want) {
		t.Errorf("Steps() kinds = %v, want %v (filters before outputs)", got, want)
	}
}

func TestPipelineFind(t *testing.T) {
	p := New(Speed{Factor: 2}, Encoder{Codec: CodecH264})
	if s, ok := p.Find(KindSpeed); !ok || s.(Speed).Factor != 2 {
		t.Errorf("Find(KindSpeed) = %+v, %v, want Speed{2}, true", s, ok)
	}
	if _, ok := p.Find(KindTrim); ok {
		t.Error("Find(KindTrim) = true, want false")
	}
}

func TestPipelineRemove(t *testing.T) {
	p := New(Resolution{Width: 1920, Height: 1080}, Speed{Factor: 2}, Trim{Start: 1, End: 3})
	p2 := p.Remove(1) // Speed, index into Steps()
	got := kindsOf(p2.Steps())
	want := []Kind{KindResolution, KindTrim}
	if !kindsEqual(got, want) {
		t.Errorf("Steps() kinds after Remove(1) = %v, want %v", got, want)
	}
	if p.Len() != 3 {
		t.Errorf("original Len() = %d, want 3 (receiver unchanged)", p.Len())
	}
}

func TestPipelineRemoveOutOfRangeNoop(t *testing.T) {
	p := New(Speed{Factor: 2})
	p2 := p.Remove(5)
	if p2.Len() != 1 {
		t.Errorf("Remove(out of range) Len() = %d, want 1 (no-op)", p2.Len())
	}
	p3 := p.Remove(-1)
	if p3.Len() != 1 {
		t.Errorf("Remove(-1) Len() = %d, want 1 (no-op)", p3.Len())
	}
}

func TestPipelineMoveAdjacentFilters(t *testing.T) {
	p := New(Resolution{Width: 1920, Height: 1080}, Speed{Factor: 2}, Trim{Start: 1, End: 3})
	p2 := p.Move(1, 1) // swap Speed and Trim
	got := kindsOf(p2.Steps())
	want := []Kind{KindResolution, KindTrim, KindSpeed}
	if !kindsEqual(got, want) {
		t.Errorf("Steps() kinds after Move(1,1) = %v, want %v", got, want)
	}
	origKinds := kindsOf(p.Steps())
	origWant := []Kind{KindResolution, KindSpeed, KindTrim}
	if !kindsEqual(origKinds, origWant) {
		t.Errorf("original Steps() kinds = %v, want %v (receiver unchanged)", origKinds, origWant)
	}
}

func TestPipelineMoveNoopAtFilterBoundary(t *testing.T) {
	p := New(Resolution{Width: 1920, Height: 1080}, Speed{Factor: 2})
	p2 := p.Move(0, -1) // already first filter
	got := kindsOf(p2.Steps())
	want := []Kind{KindResolution, KindSpeed}
	if !kindsEqual(got, want) {
		t.Errorf("Steps() kinds after Move(0,-1) = %v, want %v (no-op)", got, want)
	}
}

func TestPipelineMoveNoopOnOutputStep(t *testing.T) {
	p := New(Speed{Factor: 2}, Encoder{Codec: CodecH264}, Container{Format: FormatMP4})
	// index 1 is Encoder (an output step); moving must no-op.
	p2 := p.Move(1, 1)
	got := kindsOf(p2.Steps())
	want := []Kind{KindSpeed, KindEncoder, KindContainer}
	if !kindsEqual(got, want) {
		t.Errorf("Steps() kinds after Move on output step = %v, want %v (no-op)", got, want)
	}
}

func TestPipelineMoveOutOfRangeNoop(t *testing.T) {
	p := New(Speed{Factor: 2})
	p2 := p.Move(5, 1)
	if p2.Len() != 1 {
		t.Errorf("Move(out of range) Len() = %d, want 1 (no-op)", p2.Len())
	}
}

func TestPipelineNewUpsertsInOrder(t *testing.T) {
	// A later duplicate kind in New's args replaces the earlier one in place.
	p := New(Speed{Factor: 2}, Resolution{Width: 1920, Height: 1080}, Speed{Factor: 4})
	steps := p.Steps()
	got := kindsOf(steps)
	want := []Kind{KindSpeed, KindResolution}
	if !kindsEqual(got, want) {
		t.Errorf("Steps() kinds = %v, want %v", got, want)
	}
	if steps[0].(Speed).Factor != 4 {
		t.Errorf("Steps()[0] = %+v, want Speed{Factor:4}", steps[0])
	}
}
