package transport

type header struct {
	options []Option
}

func (h *header) Options() []Option {
	return h.options
}
