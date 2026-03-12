package ipc

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"sync"
	"time"

	"github.com/richiejp/VoxInput/internal/gui"
)

const maxReplayEvents = 100

type client struct {
	conn net.Conn
}

type Server struct {
	listener net.Listener
	mu       sync.Mutex
	clients  map[*client]struct{}
	cmdCh    chan Command
	done     chan struct{}
	replay   []Event
}

func SocketPath() string {
	dir := os.Getenv("XDG_RUNTIME_DIR")
	if dir == "" {
		dir = "/tmp"
	}
	return dir + "/VoxInput.sock"
}

func NewServer(path string) (*Server, error) {
	// Remove stale socket
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("remove stale socket: %w", err)
	}

	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, fmt.Errorf("listen on %s: %w", path, err)
	}

	s := &Server{
		listener: ln,
		clients:  make(map[*client]struct{}),
		cmdCh:    make(chan Command, 16),
		done:     make(chan struct{}),
	}

	go s.acceptLoop()

	return s, nil
}

func (s *Server) acceptLoop() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			select {
			case <-s.done:
				return
			default:
			}
			log.Printf("ipc server: accept error: %v", err)
			continue
		}

		c := &client{conn: conn}

		s.mu.Lock()
		s.clients[c] = struct{}{}
		// Send replay events to new client
		for _, e := range s.replay {
			if err := EncodeEvent(c.conn, e); err != nil {
				log.Printf("ipc server: replay error: %v", err)
				break
			}
		}
		s.mu.Unlock()

		go s.readClient(c)
	}
}

func (s *Server) readClient(c *client) {
	scanner := bufio.NewScanner(c.conn)
	for {
		cmd, err := DecodeCommand(scanner)
		if err != nil {
			if err != io.EOF {
				log.Printf("ipc server: read client error: %v", err)
			}
			s.removeClient(c)
			return
		}
		select {
		case s.cmdCh <- cmd:
		default:
			log.Println("ipc server: command channel full, dropping command")
		}
	}
}

func (s *Server) removeClient(c *client) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.clients, c)
	c.conn.Close()
}

func (s *Server) Send(msg gui.Msg) {
	e := EventFromGUIMsg(msg)
	e.Ts = time.Now().UnixMilli()
	s.Broadcast(e)
}

func (s *Server) Broadcast(e Event) {
	s.mu.Lock()

	s.replay = append(s.replay, e)
	if len(s.replay) > maxReplayEvents {
		trimmed := make([]Event, maxReplayEvents)
		copy(trimmed, s.replay[len(s.replay)-maxReplayEvents:])
		s.replay = trimmed
	}

	snapshot := make([]*client, 0, len(s.clients))
	for c := range s.clients {
		snapshot = append(snapshot, c)
	}
	s.mu.Unlock()

	var failed []*client
	for _, c := range snapshot {
		c.conn.SetWriteDeadline(time.Now().Add(100 * time.Millisecond))
		if err := EncodeEvent(c.conn, e); err != nil {
			log.Printf("ipc server: broadcast error, removing client: %v", err)
			failed = append(failed, c)
		}
	}

	if len(failed) > 0 {
		s.mu.Lock()
		for _, c := range failed {
			delete(s.clients, c)
			c.conn.Close()
		}
		s.mu.Unlock()
	}
}

func (s *Server) Commands() <-chan Command {
	return s.cmdCh
}

func (s *Server) Close() error {
	close(s.done)
	err := s.listener.Close()

	s.mu.Lock()
	for c := range s.clients {
		c.conn.Close()
		delete(s.clients, c)
	}
	s.mu.Unlock()

	// Remove socket file
	if addr, ok := s.listener.Addr().(*net.UnixAddr); ok {
		os.Remove(addr.Name)
	}

	return err
}
