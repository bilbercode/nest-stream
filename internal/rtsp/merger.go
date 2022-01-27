package rtsp

import (
	"sync"

	"github.com/pion/rtp"
)

type merger struct {
	sync.Mutex
	sequence         uint16
	lastTimestamp    uint32
	currentSessionID string
	nextSessionID    string
	subscriber       func(*rtp.Packet)
	initialSSRC      uint32
}

func (m *merger) MergeStream(in <-chan *rtp.Packet, sessionID string) {
	//var received uint16
	first := true
	for {
		m.Lock()
		input, ok := <-in
		switch {
		case !ok:
			if m.nextSessionID == sessionID {
				m.nextSessionID = ""
			}
			m.Unlock()
			return
		case input == nil:
			m.Unlock()
			continue
		case first:
			if m.initialSSRC == 0 {
				m.initialSSRC = input.SSRC
			}
			if m.nextSessionID == "" {
				m.currentSessionID = sessionID
			}
			m.nextSessionID = sessionID
			first = false
			m.Unlock()
			continue
		case sessionID != m.currentSessionID:
			m.Unlock()
			continue
		}

		if m.nextSessionID != sessionID && input.Marker {
			m.currentSessionID = m.nextSessionID
			m.Unlock()
			continue
		}

		if m.lastTimestamp > 0 && input.Timestamp <= m.lastTimestamp {
			m.Unlock()
			continue
		}

		input.SSRC = m.initialSSRC
		m.lastTimestamp = 0
		m.subscriber(input)
		m.Unlock()

	}
}
