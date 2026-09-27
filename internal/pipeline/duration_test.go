package pipeline

import "testing"

func TestOutputDuration(t *testing.T) {
	info := infoWithDuration(33)
	cases := []struct {
		name string
		p    Pipeline
		want float64
	}{
		{"none", New(), 33},
		{"speed2", New(Speed{Factor: 2}), 16.5},
		{"speed2 then trim1-3", New(Speed{Factor: 2}, Trim{Start: 1, End: 3}), 2},
		{"trim1-3 then speed2", New(Trim{Start: 1, End: 3}, Speed{Factor: 2}), 1},
		{"trim30-0", New(Trim{Start: 30}), 3},
	}
	for _, c := range cases {
		if got := OutputDuration(info, c.p); got != c.want {
			t.Errorf("%s: OutputDuration = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestPlayableRange(t *testing.T) {
	info := infoWithDuration(33)
	cases := []struct {
		name         string
		p            Pipeline
		wantS, wantE float64
	}{
		{"none", New(), 0, 33},
		{"speed2 then trim1-3", New(Speed{Factor: 2}, Trim{Start: 1, End: 3}), 2, 6},
		{"trim1-3 then speed2", New(Trim{Start: 1, End: 3}, Speed{Factor: 2}), 1, 3},
		{"trim30-0", New(Trim{Start: 30}), 30, 33},
	}
	for _, c := range cases {
		s, e := PlayableRange(info, c.p)
		if s != c.wantS || e != c.wantE {
			t.Errorf("%s: PlayableRange = [%v,%v], want [%v,%v]", c.name, s, e, c.wantS, c.wantE)
		}
	}
}

func TestPlaybackRate(t *testing.T) {
	if got := PlaybackRate(New()); got != 1 {
		t.Errorf("PlaybackRate(none) = %v, want 1", got)
	}
	if got := PlaybackRate(New(Speed{Factor: 2.5})); got != 2.5 {
		t.Errorf("PlaybackRate(speed2.5) = %v, want 2.5", got)
	}
}

func TestSpatialVideoFilter(t *testing.T) {
	if got := SpatialVideoFilter(New()); got != "" {
		t.Errorf("SpatialVideoFilter(none) = %q, want \"\"", got)
	}
	p := New(Resolution{Width: 1920, Height: 1080})
	want := Resolution{Width: 1920, Height: 1080}.VideoFilter()
	if got := SpatialVideoFilter(p); got != want {
		t.Errorf("SpatialVideoFilter = %q, want %q", got, want)
	}
}

func TestMapTime(t *testing.T) {
	cases := []struct {
		name   string
		p      Pipeline
		before int
		t      float64
		want   float64
	}{
		{"no steps", New(), 0, 10, 10},
		{"speed2 then trim", New(Speed{Factor: 2}, Trim{Start: 1, End: 3}), 2, 10, 2},
		{"trim then speed2", New(Trim{Start: 1, End: 3}, Speed{Factor: 2}), 2, 10, 1},
		{"speed4 alone", New(Speed{Factor: 4}), 1, 20, 5},
	}
	for _, c := range cases {
		if got := MapTime(c.p, c.before, c.t); got != c.want {
			t.Errorf("%s: MapTime(before=%d, t=%v) = %v, want %v", c.name, c.before, c.t, got, c.want)
		}
	}
}
