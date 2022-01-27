package transport

import "strings"

type option struct {
	unicast  bool
	protocol Protocol
	params   []Parameter
}

func (o *option) Protocol() Protocol {
	return o.protocol
}

func (o *option) IsUnicast() bool {
	return o.unicast
}

func (o *option) Parameters() []Parameter {
	return o.params
}

func (o *option) String() string {
	segments := []string{"RTP/AVP"}
	if o.protocol == ProtocolTCP {
		segments[0] += "/TCP"
	}

	for _, param := range o.params {
		segments = append(segments, param.String())
	}

	return strings.Join(segments, ";")
}
