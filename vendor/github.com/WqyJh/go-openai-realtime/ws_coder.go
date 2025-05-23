package openairt

import (
	"context"
	"io"
	"net/http"

	"github.com/coder/websocket"
)

// CoderWebSocketOptions is the options for CoderWebSocketConn.
type CoderWebSocketOptions struct {
	// ReadLimit is the maximum size of a message in bytes. -1 means no limit. Default is -1.
	ReadLimit int64
	// DialOptions is the options to pass to the websocket.Dial function.
	DialOptions *websocket.DialOptions
}

// CoderWebSocketDialer is a WebSocket dialer implementation based on coder/websocket.
type CoderWebSocketDialer struct {
	options CoderWebSocketOptions
}

// NewCoderWebSocketDialer creates a new CoderWebSocketDialer.
func NewCoderWebSocketDialer(
	options CoderWebSocketOptions,
) *CoderWebSocketDialer {
	// set default read limit
	if options.ReadLimit <= 0 {
		options.ReadLimit = -1
	}
	return &CoderWebSocketDialer{
		options: options,
	}
}

// Dial establishes a new WebSocket connection to the given URL.
func (d *CoderWebSocketDialer) Dial(ctx context.Context, url string, header http.Header) (WebSocketConn, error) {
	mergedHeader := http.Header{}
	for k, v := range header {
		mergedHeader[k] = append(mergedHeader[k], v...)
	}
	if d.options.DialOptions == nil {
		d.options.DialOptions = &websocket.DialOptions{
			HTTPHeader: mergedHeader,
		}
	} else {
		for k, v := range d.options.DialOptions.HTTPHeader {
			mergedHeader[k] = append(mergedHeader[k], v...)
		}
		d.options.DialOptions.HTTPHeader = mergedHeader
	}

	conn, resp, err := websocket.Dial(ctx, url, d.options.DialOptions)
	if resp != nil && resp.Body != nil {
		// The resp.Body is no longer needed after the dial succeeds.
		// When dial fails, the resp.Body contains the original body of the response,
		// which we don't need now.
		_ = resp.Body.Close()
	}
	if err != nil {
		return nil, err
	}

	conn.SetReadLimit(d.options.ReadLimit)

	return &CoderWebSocketConn{conn: conn, options: d.options, resp: resp}, nil
}

// CoderWebSocketConn is a WebSocket connection implementation based on coder/websocket.
type CoderWebSocketConn struct {
	conn    *websocket.Conn
	resp    *http.Response
	options CoderWebSocketOptions
}

// ReadMessage reads a message from the WebSocket connection.
//
// The ctx could be used to cancel the read operation. If the ctx is canceled or timedout,
// the read operation will be canceled and the connection will be closed.
//
// If the returned error is Permanent, the future read operations on the same connection will not succeed.
func (c *CoderWebSocketConn) ReadMessage(ctx context.Context) (MessageType, []byte, error) {
	messageType, r, err := c.conn.Reader(ctx)
	if err != nil {
		return 0, nil, Permanent(err)
	}

	data, err := io.ReadAll(r)
	if err != nil {
		return 0, nil, Permanent(err)
	}

	switch messageType {
	case websocket.MessageText:
		return MessageText, data, nil
	case websocket.MessageBinary:
		return MessageBinary, data, nil
	default:
		return 0, nil, ErrUnsupportedMessageType
	}
}

// WriteMessage writes a message to the WebSocket connection.
//
// The ctx could be used to cancel the write operation. If the ctx is canceled or timedout,
// the write operation will be canceled and the connection will be closed.
//
// If the returned error is Permanent, the future write operations on the same connection will not succeed.
func (c *CoderWebSocketConn) WriteMessage(ctx context.Context, messageType MessageType, data []byte) error {
	switch messageType {
	case MessageText:
		return Permanent(c.conn.Write(ctx, websocket.MessageText, data))
	case MessageBinary:
		return Permanent(c.conn.Write(ctx, websocket.MessageBinary, data))
	default:
		return ErrUnsupportedMessageType
	}
}

// Close closes the WebSocket connection.
func (c *CoderWebSocketConn) Close() error {
	return c.conn.Close(websocket.StatusNormalClosure, "")
}

// Response returns the *http.Response of the WebSocket connection.
// Commonly used to get response headers.
func (c *CoderWebSocketConn) Response() *http.Response {
	return c.resp
}

// Ping sends a ping message to the WebSocket connection.
// It would be blocked until the pong message is received or the ctx is done.
func (c *CoderWebSocketConn) Ping(ctx context.Context) error {
	return c.conn.Ping(ctx)
}
