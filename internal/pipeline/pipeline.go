package pipeline

// outputOrder is the canonical order in which output steps are listed,
// independent of the order they were upserted.
var outputOrder = []Kind{KindEncoder, KindQuality, KindAudio, KindContainer, KindRawArgs}

// Pipeline is an immutable ordered set of filter steps plus a set of output
// settings. Every method returns a new value; the receiver is untouched,
// which is what makes undo in the TUI a plain stack of prior values.
type Pipeline struct {
	filters []Step
	outputs map[Kind]Step
}

// New builds a Pipeline by upserting steps in order.
func New(steps ...Step) Pipeline {
	var p Pipeline
	for _, s := range steps {
		p = p.Upsert(s)
	}
	return p
}

// Steps returns the filter steps in their current order, followed by the
// output steps in canonical order (Encoder, Quality, Audio, Container,
// RawArgs). The returned slice is a copy.
func (p Pipeline) Steps() []Step {
	result := make([]Step, 0, len(p.filters)+len(p.outputs))
	result = append(result, p.filters...)
	for _, k := range outputOrder {
		if s, ok := p.outputs[k]; ok {
			result = append(result, s)
		}
	}
	return result
}

// Len returns the total number of steps.
func (p Pipeline) Len() int {
	return len(p.filters) + len(p.outputs)
}

// Find returns the step of kind k, if present.
func (p Pipeline) Find(k Kind) (Step, bool) {
	if k.IsFilter() {
		for _, s := range p.filters {
			if s.Kind() == k {
				return s, true
			}
		}
		return nil, false
	}
	s, ok := p.outputs[k]
	return s, ok
}

// Upsert adds s, or replaces the existing step of the same kind in place.
// A new filter step is appended after the last filter step.
func (p Pipeline) Upsert(s Step) Pipeline {
	np := p.clone()
	k := s.Kind()
	if k.IsFilter() {
		for i, existing := range np.filters {
			if existing.Kind() == k {
				np.filters[i] = s
				return np
			}
		}
		np.filters = append(np.filters, s)
		return np
	}
	np.outputs[k] = s
	return np
}

// Remove deletes the step at Steps()[i]. Out-of-range i is a no-op.
func (p Pipeline) Remove(i int) Pipeline {
	steps := p.Steps()
	if i < 0 || i >= len(steps) {
		return p
	}
	np := p.clone()
	if i < len(np.filters) {
		// Steps() lists filters first, in np.filters' own order, so index
		// i already addresses np.filters directly.
		np.filters = append(np.filters[:i], np.filters[i+1:]...)
		return np
	}
	delete(np.outputs, steps[i].Kind())
	return np
}

// Move swaps the filter step at Steps()[i] with its neighbour at i+delta.
// It no-ops on an output step, out-of-range indexes, or at the filter
// boundary (there is no adjacent filter to swap with).
func (p Pipeline) Move(i, delta int) Pipeline {
	steps := p.Steps()
	if i < 0 || i >= len(steps) || !steps[i].Kind().IsFilter() {
		return p
	}
	j := i + delta
	if j < 0 || j >= len(p.filters) {
		return p
	}
	np := p.clone()
	np.filters[i], np.filters[j] = np.filters[j], np.filters[i]
	return np
}

func (p Pipeline) clone() Pipeline {
	np := Pipeline{
		filters: make([]Step, len(p.filters)),
		outputs: make(map[Kind]Step, len(p.outputs)),
	}
	copy(np.filters, p.filters)
	for k, v := range p.outputs {
		np.outputs[k] = v
	}
	return np
}
