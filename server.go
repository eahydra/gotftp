package gotftp

import (
	"io"
	"net"
	"sync"
	"time"
)

type ReadSeekCloser interface {
	io.Reader
	io.Seeker
	io.Closer
}

type WriteSeekCloser interface {
	io.Writer
	io.Seeker
	io.Closer
}

type FileHandler interface {
	ReadFile(remoteAddr, fileName string) (ReadSeekCloser, error)
	WriteFile(remoteAddr, fileName string) (WriteSeekCloser, error)
	IsFileExist(remoteAddr, fileName string) (exist bool, err error)
}

type clientPacket struct {
	data       []byte
	remoteAddr net.Addr
}

type Server struct {
	closed      bool
	conn        net.PacketConn
	fileHandler FileHandler
	readTimeout time.Duration
	packetChan  chan clientPacket
	done        chan struct{}
	peerMap     map[string]*clientPeer
	lock        sync.Mutex
	pool        *sync.Pool
}

func allocateBuffer() interface{} {
	return make([]byte, 1024)
}

func NewServer(addr string, fileHandler FileHandler, readTimeout time.Duration) (*Server, error) {
	conn, err := net.ListenPacket("udp4", addr)
	if err != nil {
		return nil, err
	}
	return &Server{
		conn:        conn,
		fileHandler: fileHandler,
		readTimeout: readTimeout,
		done:        make(chan struct{}, 1),
		packetChan:  make(chan clientPacket, 1024),
		peerMap:     make(map[string]*clientPeer),
		pool:        &sync.Pool{New: allocateBuffer},
	}, nil
}

func (s *Server) Close() error {
	if s.closed {
		return nil
	}
	s.closed = true
	close(s.done)
	return s.conn.Close()
}

func (s *Server) removeClientPeer() {
	for {
		select {
		case <-time.After(time.Duration(100) * time.Millisecond):
			{
				now := time.Now()
				s.lock.Lock()
				for k, v := range s.peerMap {
					if now.Sub(v.keepaliveTime) > time.Duration(v.timeout)*time.Second {
						logln("timeout, remote:", v.remoteAddr.String())
						v.Close()
						delete(s.peerMap, k)
					}
				}
				s.lock.Unlock()
			}
		case <-s.done:
			{
				return
			}
		}
	}
}

func (s *Server) work() {
	for {
		select {
		case r, ok := <-s.packetChan:
			{
				if ok {
					var p *clientPeer
					s.lock.Lock()
					if p, ok = s.peerMap[r.remoteAddr.String()]; !ok {
						p = newClientPeer(r.remoteAddr, s.fileHandler)
						s.peerMap[r.remoteAddr.String()] = p
					}
					s.lock.Unlock()
					p.Dispatch(s.conn, r.data)
					s.pool.Put(r.data[:cap(r.data)])
				}
			}
		case <-s.done:
			{
				return
			}
		}
	}
}

func (s *Server) Run() error {
	go s.removeClientPeer()
	go s.work()
	for {
		if s.readTimeout != 0 {
			s.conn.SetReadDeadline(time.Now().Add(s.readTimeout))
		}

		buff := s.pool.Get().([]byte)
		n, raddr, err := s.conn.ReadFrom(buff)
		if err != nil {
			if netErr, ok := err.(net.Error); ok {
				if netErr.Timeout() {
					s.pool.Put(buff)
					continue
				}
			}
			return err
		}

		select {
		case <-s.done:
			return nil
		case s.packetChan <- clientPacket{buff[:n], raddr}:
		}
	}
	return nil
}
