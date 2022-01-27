package rtsp

import (
	"sync"

	"github.com/pion/sdp/v3"

	"github.com/pion/rtcp"
	"github.com/pion/rtp"
)

type cameraSubscriber struct {
	ready bool
	rtcp  func(packet rtcp.Packet)
	rtp   func(packet *rtp.Packet)
	seq   uint16
}

type camera struct {
	sync.Mutex
	rtpMerger   *merger
	description *sdp.MediaDescription
	subscribers map[string]*cameraSubscriber
}

func NewCamera() *camera {
	m := &merger{
		Mutex: sync.Mutex{},
	}
	cam := &camera{
		Mutex:       sync.Mutex{},
		rtpMerger:   m,
		subscribers: make(map[string]*cameraSubscriber),
	}

	m.subscriber = func(packet *rtp.Packet) {
		cam.Lock()
		defer cam.Unlock()
		for _, subscriber := range cam.subscribers {
			if subscriber.ready {
				subscriber.rtp(packet)
			}
		}
	}

	return cam
}

func (c *camera) RegisterSubscriber(id string, rtpH func(packet *rtp.Packet), rtcpH func(packet rtcp.Packet)) {
	c.Lock()
	defer c.Unlock()
	c.subscribers[id] = &cameraSubscriber{
		rtcp: rtcpH,
		seq:  1,
	}
	c.subscribers[id].rtp = func(packet *rtp.Packet) {
		if c.subscribers[id].seq == 65535 {
			c.subscribers[id].seq = 1
		} else {
			c.subscribers[id].seq++
		}
		if packet != nil {
			p := *packet
			p.SequenceNumber = c.subscribers[id].seq
			rtpH(&p)
		}
	}
}

func (c *camera) Teardown(id string) {
	c.Lock()
	defer c.Unlock()
	delete(c.subscribers, id)
}

func (c *camera) Play(id string) {
	c.Lock()
	defer c.Unlock()
	//TODO (bilbercode) start sending RTCP packets for subscriber
	c.subscribers[id].ready = true
}

func (c *camera) MergeStream(in <-chan *rtp.Packet, sessionID string) {
	c.rtpMerger.MergeStream(in, sessionID)
}
