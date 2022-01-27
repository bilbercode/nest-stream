package rtsp

import (
	"context"
	"net"

	"github.com/pion/rtp"
	"github.com/pion/sdp/v3"
)

type Camera interface {
	MergeStream(in <-chan *rtp.Packet, sessionID string)
}

type Server interface {
	RegisterCamera(name string, d *sdp.MediaDescription) Camera
	Start(ctx context.Context, addr string) error
}

type Client interface {
	SendRequest(ctx context.Context, request *Request) (*Response, error)
	SendResponse(ctx context.Context, response *Response) error
	SubscribeRequests(h func(request *Request, c Client) error) func()
	SubscribeInterleavedFrames(h func(channel uint8, payload []byte)) func()

	Conn() net.Conn

	Done() <-chan struct{}
	Err() error
}
