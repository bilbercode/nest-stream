package transport

import (
	"fmt"
	"strconv"
	"time"
)

type Destination string

func (p Destination) String() string {
	if p == "" {
		return "destination"
	}
	return "destination=" + string(p)
}

type Interleaved []int

func (p Interleaved) String() string {
	if len(p) == 1 {
		return fmt.Sprintf("interleaved=%d", p[0])
	}
	return fmt.Sprintf("interleaved=%d-%d", p[0], p[1])
}

type Append string

func (p Append) String() string {
	return string(p)
}

type TTL time.Duration

func (p TTL) String() string {
	return fmt.Sprintf("ttl=%d", time.Duration(p)/time.Second)
}

type Layers int

func (p Layers) String() string {
	return fmt.Sprintf("layers=%d", p)
}

type Port []int

func (p Port) String() string {
	if len(p) == 1 {
		return fmt.Sprintf("port=%d", p[0])
	}
	return fmt.Sprintf("port=%d-%d", p[0], p[2])
}

type ClientPort []int

func (p ClientPort) String() string {
	if len(p) == 1 {
		return fmt.Sprintf("client_port=%d", p[0])
	}
	return fmt.Sprintf("port=%d-%d", p[0], p[2])
}

type ServerPort []int

func (p ServerPort) String() string {
	if len(p) == 1 {
		return fmt.Sprintf("port=%d", p[0])
	}
	return fmt.Sprintf("server_port=%d-%d", p[0], p[2])
}

type SSRC int

func (p SSRC) String() string {
	return "ssrc=" + strconv.FormatInt(int64(p), 10)
}

type Mode string

func (p Mode) String() string {
	return "mode=" + string(p)
}
