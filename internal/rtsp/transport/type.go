package transport

import "errors"

type Protocol string

const (
	ProtocolUDP Protocol = "UDP"
	ProtocolTCP Protocol = "TCP"
)

const (
	UnsupportedTransportMessage = "Unsupported Transport"
	UnsupportedTransportCode    = 461
)

var (
	ErrUnsupportedTransport = errors.New("unsupported transport")
)

type Header interface {
	Options() []Option
}

type Option interface {
	IsUnicast() bool
	Protocol() Protocol
	Parameters() []Parameter
	String() string
}

type Parameter interface {
	String() string
}
