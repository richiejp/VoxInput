package ipc

import (
	"bufio"
	"fmt"
	"net"
)

type Client struct {
	conn    net.Conn
	scanner *bufio.Scanner
}

func Connect(path string) (*Client, error) {
	conn, err := net.Dial("unix", path)
	if err != nil {
		return nil, fmt.Errorf("connect to %s: %w", path, err)
	}

	return &Client{
		conn:    conn,
		scanner: bufio.NewScanner(conn),
	}, nil
}

func (c *Client) ReadEvent() (Event, error) {
	return DecodeEvent(c.scanner)
}

func (c *Client) SendCommand(cmd Command) error {
	return EncodeCommand(c.conn, cmd)
}

func (c *Client) Close() error {
	return c.conn.Close()
}
