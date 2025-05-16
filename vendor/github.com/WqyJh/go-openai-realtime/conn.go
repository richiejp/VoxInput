package openairt

import (
	"context"
	"errors"
	"fmt"
)

type ServerEventHandler func(ctx context.Context, event ServerEvent)

// Conn is a connection to the OpenAI Realtime API.
type Conn struct {
	logger Logger
	conn   WebSocketConn
}

// Close closes the connection.
func (c *Conn) Close() error {
	return c.conn.Close()
}

// SendMessageRaw sends a raw message to the server.
func (c *Conn) SendMessageRaw(ctx context.Context, data []byte) error {
	return c.conn.WriteMessage(ctx, MessageText, data)
}

// SendMessage sends a client event to the server.
func (c *Conn) SendMessage(ctx context.Context, msg ClientEvent) error {
	data, err := MarshalClientEvent(msg)
	if err != nil {
		return err
	}
	return c.SendMessageRaw(ctx, data)
}

// ReadMessageRaw reads a raw message from the server.
func (c *Conn) ReadMessageRaw(ctx context.Context) ([]byte, error) {
	messageType, data, err := c.conn.ReadMessage(ctx)
	if err != nil {
		return nil, err
	}
	if messageType != MessageText {
		return nil, fmt.Errorf("expected text message, got %d", messageType)
	}
	return data, nil
}

// ReadMessage reads a server event from the server.
func (c *Conn) ReadMessage(ctx context.Context) (ServerEvent, error) {
	data, err := c.ReadMessageRaw(ctx)
	if err != nil {
		return nil, err
	}
	event, err := UnmarshalServerEvent(data)
	if err != nil {
		return nil, err
	}
	return event, nil
}

// Ping sends a ping message to the WebSocket connection.
func (c *Conn) Ping(ctx context.Context) error {
	return c.conn.Ping(ctx)
}

// ConnHandler is a handler for a connection to the OpenAI Realtime API.
// It reads messages from the server in a standalone goroutine and calls the registered handlers.
// It is the responsibility of the caller to call Start and Stop.
// The handlers are called in the order they are registered.
// Users should not call ReadMessage directly when using ConnHandler.
type ConnHandler struct {
	ctx      context.Context
	conn     *Conn
	handlers []ServerEventHandler
	errCh    chan error
}

// NewConnHandler creates a new ConnHandler with the given context and connection.
func NewConnHandler(ctx context.Context, conn *Conn, handlers ...ServerEventHandler) *ConnHandler {
	return &ConnHandler{
		ctx:      ctx,
		conn:     conn,
		handlers: handlers,
		errCh:    make(chan error, 1),
	}
}

// Start starts the ConnHandler.
func (c *ConnHandler) Start() {
	go func() {
		err := c.run()
		if err != nil {
			c.errCh <- err
		}
		close(c.errCh)
	}()
}

// Err returns a channel that receives errors from the ConnHandler.
// This could be used to wait for the goroutine to exit.
// If you don't need to wait for the goroutine to exit, there's no need to call this.
// This must be called after the connection is closed, otherwise it will block indefinitely.
func (c *ConnHandler) Err() <-chan error {
	return c.errCh
}

func (c *ConnHandler) run() error {
	for {
		select {
		case <-c.ctx.Done():
			return c.ctx.Err()
		default:
		}

		msg, err := c.conn.ReadMessage(c.ctx)
		if err != nil {
			var permanent *PermanentError
			if errors.As(err, &permanent) {
				return permanent.Err
			}
			c.conn.logger.Warnf("read message temporary error: %+v", err)
			continue
		}
		for _, handler := range c.handlers {
			handler(c.ctx, msg)
		}
	}
}
