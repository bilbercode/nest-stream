package rtsp

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/textproto"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"golang.org/x/sync/errgroup"
)

var (
	ErrPortConflict    = errors.New("port conflict")
	ErrChannelConflict = errors.New("channel conflict")
)

type client struct {
	sync.RWMutex
	ctx    context.Context
	cancel context.CancelFunc

	mtu int

	requestSubscribers          map[string]func(request *Request, c Client) error
	interleavedFrameSubscribers map[string]func(channel uint8, payload []byte)
	rtspSocket                  net.Conn
	requestQueue                *requestQueue

	err error
}

type requestQueue struct {
	mu    sync.Mutex
	items map[string]func(response *Response)
}

func NewClientWithContextCancel(nc net.Conn, ctx context.Context, cancel context.CancelFunc) Client {
	c := &client{
		ctx:                         ctx,
		cancel:                      cancel,
		requestQueue:                newRequestQueue(),
		requestSubscribers:          make(map[string]func(request *Request, c Client) error),
		interleavedFrameSubscribers: make(map[string]func(channel uint8, payload []byte)),
		mtu:                         4096,
		rtspSocket:                  nc,
	}
	go c.readLoop()

	return c
}

func (c *client) Conn() net.Conn {
	return c.rtspSocket
}

func (c *client) SendRequest(ctx context.Context, request *Request) (*Response, error) {
	c.Lock()
	done := make(chan struct{})
	var response *Response
	err := c.requestQueue.Enqueue(request.Sequence, func(r *Response) {
		response = r
		close(done)
	})
	if err != nil {
		c.Unlock()
		close(done)
		return nil, fmt.Errorf("failed to enqueue request: %w", err)
	}

	group, ctx := errgroup.WithContext(ctx)
	group.Go(func() error {
		select {
		case <-ctx.Done():
		case <-done:
		}
		return nil
	})

	group.Go(func() error {
		err := request.Write(c.rtspSocket)
		c.Unlock()
		if err != nil {
			return err
		}
		return nil
	})

	err = group.Wait()
	if err != nil {
		return nil, err
	}

	return response, err
}

func (c *client) SendResponse(ctx context.Context, response *Response) error {
	c.Lock()
	defer c.Unlock()
	return response.Write(c.rtspSocket)
}

func (c *client) SubscribeRequests(h func(request *Request, c Client) error) func() {
	c.Lock()
	defer c.Unlock()
	id := uuid.NewString()
	c.requestSubscribers[id] = h
	return func() {
		c.Lock()
		defer c.Unlock()
		delete(c.requestSubscribers, id)
	}
}

func (c *client) SubscribeInterleavedFrames(h func(channel uint8, payload []byte)) func() {
	c.Lock()
	defer c.Unlock()
	id := uuid.NewString()
	c.interleavedFrameSubscribers[id] = h
	return func() {
		c.Lock()
		defer c.Unlock()
		delete(c.interleavedFrameSubscribers, id)
	}
}

func (c *client) Done() <-chan struct{} {
	return c.ctx.Done()
}

func (c *client) readLoop() {
	isTCP := false
	switch c.rtspSocket.(type) {
	case *net.TCPConn:
		isTCP = true
	case *tls.Conn:
		isTCP = true
	}

	br := bufio.NewReaderSize(c.rtspSocket, c.mtu)
	reader := textproto.NewReader(br)
	for {
		_ = c.rtspSocket.SetReadDeadline(time.Now().Add(time.Second))
		// c.RLock()
		first, err := br.ReadByte()
		switch et := err.(type) {
		case net.Error:
			if et.Timeout() {
				// c.RUnlock()
				continue
			}
			c.err = err
			c.cancel()
			// c.RUnlock()
			return
		case nil:
			break
		default:

			c.err = err
			c.cancel()
			_ = c.rtspSocket.Close()
			// c.RUnlock()
			return
		}
		err = br.UnreadByte()
		if err != nil {
			c.err = fmt.Errorf("failed to unread socket input byte: %w", err)
			c.cancel()
			// c.RUnlock()
			return
		}

		if isTCP && first == 0x24 {
			// interleaved frame
			header := make([]byte, 4)
			i, err := io.ReadFull(br, header)
			switch {
			case err != nil:
				c.err = fmt.Errorf("failed to read interleaved frame header: %w", err)
				c.cancel()
				// c.RUnlock()
				return
			case i != 4:
				c.err = errors.New("failed to read interleaved frame header, short read")
				c.cancel()
				// c.RUnlock()
				return
			}

			channel := header[1]
			length := binary.BigEndian.Uint16(header[2:])
			payload := make([]byte, length)
			i, err = io.ReadFull(br, payload)
			switch {
			case err != nil:
				c.err = fmt.Errorf("failed to read interleaved frame payload: %w", err)
				c.cancel()
				// c.RUnlock()
				return
			case i != int(length):
				c.err = fmt.Errorf(
					"failed to read interleaved frame payload, short read expected %d, read %d", length, i)
				c.cancel()
				// c.RUnlock()
				return
			}

			for _, handler := range c.interleavedFrameSubscribers {
				handler(channel, payload)
			}
			// c.RUnlock()
			continue
		}

		statusLine, err := reader.ReadLine()
		if err != nil {
			c.err = fmt.Errorf("failed to read RTSP status line: %w", err)
			c.cancel()
			// c.RUnlock()
			return
		}
		var headers map[string][]string
		headers, err = reader.ReadMIMEHeader()
		if err != nil {
			c.err = fmt.Errorf("failed to read RTSP headers: %w", err)
			c.cancel()
			// c.RUnlock()
			return
		}
		var body io.ReadWriter
		if lengthHeader, ok := headers[http.CanonicalHeaderKey("Content-Length")]; ok {
			length, err := strconv.Atoi(lengthHeader[0])
			if err != nil {
				c.err = fmt.Errorf("failed to parse content-length: %w", err)
				// c.RUnlock()
				c.cancel()
				return
			}
			b := make([]byte, length)
			i, err := io.ReadFull(br, b)
			switch {
			case err != nil:
				c.err = fmt.Errorf("failed to read body of RTSP: %w", err)
				// c.RUnlock()
				c.cancel()
				return
			case i != length:
				c.err = fmt.Errorf(
					"failed to read body of RTSP: short read, expected %d read %d", length, i)
				// c.RUnlock()
				c.cancel()
				return
			}
			body = bytes.NewBuffer(b)
		}

		cSeqStr := ""
		if seqHeaders, ok := headers[http.CanonicalHeaderKey("CSeq")]; ok {
			cSeqStr = seqHeaders[0]
		}

		if strings.HasPrefix(statusLine, "RTSP") {
			// This is a response
			if !c.requestQueue.Has(cSeqStr) {
				c.err = fmt.Errorf("failed to read body of RTSP: %w", err)
				// c.RUnlock()
				c.cancel()
				return
			}

			hf, _ := c.requestQueue.Dequeue(cSeqStr)
			parts := strings.Split(statusLine, " ")
			code, err := strconv.Atoi(parts[1])
			if err != nil {
				c.err = fmt.Errorf("failed to parse response code: %w", err)
				// c.RUnlock()
				c.cancel()
				return
			}
			message := parts[2]
			parts = strings.Split(parts[0], "/")
			version := parts[1]
			hf(&Response{
				Version:  version,
				Code:     code,
				Message:  message,
				Sequence: cSeqStr,
				Header:   headers,
				Body:     body,
			})
			// c.RUnlock()
			continue
		}

		parts := strings.Split(statusLine, " ")
		method := Method(parts[0])
		addr := parts[1]
		parts = strings.Split(parts[2], "/")
		version := parts[1]

		for _, hFn := range c.requestSubscribers {

			go func() {
				err = hFn(&Request{
					Version:  version,
					Url:      addr,
					Sequence: cSeqStr,
					Method:   method,
					Header:   headers,
					Body:     body,
				}, c)
				if err != nil {
					c.err = fmt.Errorf("handler function failed with: %w", err)
					c.cancel()
					return
				}
			}()
		}
		// c.RUnlock()
	}
}
func (c *client) Err() error {
	return c.err
}

func newRequestQueue() *requestQueue {
	return &requestQueue{
		mu:    sync.Mutex{},
		items: make(map[string]func(response *Response)),
	}
}

func (r *requestQueue) Has(key string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.items[key]
	return ok
}

func (r *requestQueue) Enqueue(key string, h func(response *Response)) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.items[key]; ok {
		return errors.New("duplicate key")
	}
	r.items[key] = h
	return nil
}

func (r *requestQueue) Dequeue(key string) (func(request *Response), error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.items[key]; !ok {
		return nil, errors.New("missing")
	}
	h := r.items[key]
	delete(r.items, key)
	return h, nil
}
