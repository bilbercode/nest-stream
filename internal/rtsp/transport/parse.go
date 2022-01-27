package transport

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

func Parse(options []string) (Header, error) {
	var opts []Option
	for _, option := range options {
		o, err := parseOption(option)
		if err != nil {
			return nil, err
		}
		opts = append(opts, o)
	}

	return &header{options: opts}, nil
}

func parseOption(in string) (Option, error) {
	parts := strings.Split(in, ";")
	if len(parts) < 1 {
		return nil, errors.New("malformed transport header")
	}
	opt := &option{}
	switch parts[0] {
	case "RTP/AVP", "RTP/AVP/UDP":
		opt.protocol = ProtocolUDP
	case "RTP/AVP/TCP":
		opt.protocol = ProtocolTCP
	default:
		return nil, ErrUnsupportedTransport
	}

	for _, part := range parts[1:] {
		switch {
		case part == "unicast":
			opt.unicast = true
		case part == "multicast":
			continue
		case strings.Contains(part, "destination"):
			dParts := strings.Split(part, "=")
			if len(dParts) == 1 {
				opt.params = append(opt.params, Destination(""))
				continue
			}
			opt.params = append(opt.params, Destination(dParts[1]))
		case strings.Contains(part, "interleaved"):
			dParts := strings.Split(part, "=")
			if len(dParts) == 1 {
				return nil, errors.New(
					"malformed parameter interleaved expected at least one channel")
			}
			var cb []int
			channels := strings.Split(dParts[1], "-")
			for _, channel := range channels {
				c, err := strconv.Atoi(channel)
				if err != nil {
					return nil, fmt.Errorf(
						"failed to parse channel for interleaved frame, received %s: %w", channel, err)
				}
				cb = append(cb, c)
			}
			opt.params = append(opt.params, Interleaved(cb))
		case part == "append":
			opt.params = append(opt.params, Append(""))
		case strings.Contains(part, "ttl"):
			dParts := strings.Split(part, "=")
			if len(dParts) == 1 {
				return nil, errors.New(
					"malformed parameter ttl expected time in seconds")
			}
			seconds, err := strconv.Atoi(dParts[1])
			if err != nil {
				return nil, fmt.Errorf("failed to parse TTL value: %w", err)
			}

			opt.params = append(opt.params, TTL(time.Second*time.Duration(seconds)))
		case strings.Contains(part, "layers"):
			dParts := strings.Split(part, "=")
			if len(dParts) == 1 {
				return nil, errors.New(
					"malformed parameter layers expected layer count for multicast")
			}

			layers, err := strconv.Atoi(dParts[1])
			if err != nil {
				return nil, fmt.Errorf("failed to parse layers value: %w", err)
			}
			opt.params = append(opt.params, Layers(layers))
		case strings.Contains(part, "client_port"):
			dParts := strings.Split(part, "=")
			if len(dParts) == 1 {
				return nil, errors.New(
					"malformed parameter client_port expected at least one")
			}
			var cb []int
			ports := strings.Split(dParts[1], "-")
			for _, port := range ports {
				c, err := strconv.Atoi(port)
				if err != nil {
					return nil, fmt.Errorf(
						"failed to parse client_port, received %s: %w", port, err)
				}
				cb = append(cb, c)
			}
			opt.params = append(opt.params, ClientPort(cb))
		case strings.Contains(part, "server_port"):
			dParts := strings.Split(part, "=")
			if len(dParts) == 1 {
				return nil, errors.New(
					"malformed parameter server_port expected at least one")
			}
			var cb []int
			ports := strings.Split(dParts[1], "-")
			for _, port := range ports {
				c, err := strconv.Atoi(port)
				if err != nil {
					return nil, fmt.Errorf(
						"failed to parse server_port, received %s: %w", port, err)
				}
				cb = append(cb, c)
			}
			opt.params = append(opt.params, ServerPort(cb))
		case strings.Contains(part, "port"):
			dParts := strings.Split(part, "=")
			if len(dParts) == 1 {
				return nil, errors.New(
					"malformed parameter port expected at least one")
			}
			var cb []int
			ports := strings.Split(dParts[1], "-")
			for _, port := range ports {
				c, err := strconv.Atoi(port)
				if err != nil {
					return nil, fmt.Errorf(
						"failed to parse port, received %s: %w", port, err)
				}
				cb = append(cb, c)
			}
			opt.params = append(opt.params, Port(cb))
		case strings.Contains(part, "ssrc"):
			dParts := strings.Split(part, "=")
			if len(dParts) == 1 {
				return nil, errors.New(
					"malformed parameter ssrc expected identifier")
			}

			layers, err := strconv.Atoi(dParts[1])
			if err != nil {
				return nil, fmt.Errorf("failed to parse ssrc value: %w", err)
			}
			opt.params = append(opt.params, SSRC(layers))
		case strings.Contains(part, "mode"):
			dParts := strings.Split(part, "=")
			if len(dParts) == 1 {
				return nil, errors.New(
					"malformed parameter mode")
			}
			opt.params = append(opt.params, Mode(dParts[1]))
		default:
			return nil, fmt.Errorf("unexpected parameter %s", part)
		}
	}
	return opt, nil
}
