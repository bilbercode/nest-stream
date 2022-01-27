package rtsp

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/textproto"
	"time"
)

type Request struct {
	Version  string
	Url      string
	Sequence string
	Method   Method
	Header   http.Header
	Body     io.ReadWriter
}

func (r *Request) Write(w io.Writer) error {
	writer := textproto.NewWriter(bufio.NewWriter(w))

	err := writer.PrintfLine("%s %s RTSP/%s", r.Method, r.Url, r.Version)
	if err != nil {
		return fmt.Errorf("failed to write response line: %w", err)
	}
	if r.Header == nil {
		r.Header = http.Header{}
	}

	r.Header.Set("CSeq", r.Sequence)
	r.Header.Set("Date", time.Now().Format(http.TimeFormat))

	var body *bytes.Buffer
	if r.Body != nil {
		body = r.Body.(*bytes.Buffer)
	}

	if body != nil {
		contentLength := body.Len()
		r.Header.Set("Content-Length", fmt.Sprintf("%d", contentLength))
	}

	err = r.Header.WriteSubset(writer.W, nil)
	if err != nil {
		return err
	}

	writer.PrintfLine("")

	if body != nil {
		_, err := writer.W.Write(body.Bytes())
		if err != nil {
			return err
		}
		writer.W.Flush()
	}
	return nil
}
