package openairt

import (
	"context"
	"errors"
	"net/http"
)

// MessageType represents the type of a WebSocket message.
// See https://tools.ietf.org/html/rfc6455#section-5.6
type MessageType int

// MessageType constants.
const (
	// MessageText is for UTF-8 encoded text messages like JSON.
	MessageText MessageType = iota + 1
	// MessageBinary is for binary messages like protobufs.
	MessageBinary
)

// WebSocketConn is a WebSocket connection abstraction.
type WebSocketConn interface {
	// ReadMessage reads a message from the WebSocket connection.
	//
	// The ctx could be used to cancel the read operation. It's behavior depends on the underlying implementation.
	// If the read succeeds, the returned error should be nil, and the ctx's cancel/timeout shouldn't affect the
	// connection and future read operations.
	//
	// If the returned error is Permanent, the future read operations on the same connection will not succeed,
	// that means the connection is broken and should be closed or had already been closed.
	//
	// In general, once the ctx is canceled before read finishes, the read operation will be canceled and
	// the connection will be closed.
	//
	// There are some exceptions:
	// - If the underlying implementation is gorilla/websocket, the read operation will not be canceled
	//   when the ctx is canceled before its deadline, it will keep reading until the ctx reaches deadline or the connection is closed.
	ReadMessage(ctx context.Context) (messageType MessageType, p []byte, err error)

	// WriteMessage writes a message to the WebSocket connection.
	//
	// The ctx could be used to cancel the write operation. It's behavior depends on the underlying implementation.
	//
	// If the returned error is Permanent, the future write operations on the same connection will not succeed,
	// that means the connection is broken and should be closed or had already been closed.
	//
	// In general, once the ctx is canceled before write finishes, the write operation will be canceled and
	// the connection will be closed.
	WriteMessage(ctx context.Context, messageType MessageType, data []byte) error

	// Close closes the WebSocket connection.
	Close() error

	// Response returns the *http.Response of the WebSocket connection.
	// Commonly used to get response headers.
	Response() *http.Response

	// Ping sends a ping message to the WebSocket connection.
	Ping(ctx context.Context) error
}

// WebSocketDialer is a WebSocket connection dialer abstraction.
type WebSocketDialer interface {
	// Dial establishes a new WebSocket connection to the given URL.
	// The ctx could be used to cancel the dial operation. It's effect depends on the underlying implementation.
	Dial(ctx context.Context, url string, header http.Header) (WebSocketConn, error)
}

// DefaultDialer returns a default WebSocketDialer.
func DefaultDialer() WebSocketDialer {
	return NewCoderWebSocketDialer(CoderWebSocketOptions{})
}

var (
	ErrUnsupportedMessageType = errors.New("unsupported message type")
)
