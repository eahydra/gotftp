/*
 * Copyright (c) 2013 author: LiTao
 * All rights reserved.
 *
 * Redistribution and use in source and binary forms, with or without
 * modification, are permitted provided that the following conditions
 * are met:
 * 1. Redistributions of source code must retain the above copyright
 *    notice, this list of conditions and the following disclaimer.
 * 2. Redistributions in binary form must reproduce the above copyright
 *    notice, this list of conditions and the following disclaimer in the
 *    documentation and/or other materials provided with the distribution.
 * 3. All advertising materials mentioning features or use of this software
 *    must display the following acknowledgement:
 *	This product includes software developed by the University of
 *	California, Berkeley and its contributors.
 * 4. Neither the name of the University nor the names of its contributors
 *    may be used to endorse or promote products derived from this software
 *    without specific prior written permission.
 *
 * THIS SOFTWARE IS PROVIDED BY THE REGENTS AND CONTRIBUTORS ``AS IS'' AND
 * ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE
 * IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE
 * ARE DISCLAIMED.  IN NO EVENT SHALL THE REGENTS OR CONTRIBUTORS BE LIABLE
 * FOR ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR CONSEQUENTIAL
 * DAMAGES (INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS
 * OR SERVICES; LOSS OF USE, DATA, OR PROFITS; OR BUSINESS INTERRUPTION)
 * HOWEVER CAUSED AND ON ANY THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT
 * LIABILITY, OR TORT (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY
 * OUT OF THE USE OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF
 * SUCH DAMAGE.
 */

package gotftp

import (
	"io"
	"net"
	"time"
)

// ReadCloser - contains io.ReadCloser. Used to read data and close.
type ReadCloser interface {
	io.ReadCloser
	Size() (n int64, err error)
}

// WriteCloser - contains io.WriteCloser. Used to write data and close when finish.
type WriteCloser interface {
	io.WriteCloser
}

// ServerHandler - handle TFTP request.
type ServerHandler interface {
	// ReadFile - process RRQ. The param file is target file path
	ReadFile(file string) (f ReadCloser, err error)
	// WrieFile - process WRQ. The param file is targe file path
	WriteFile(file string) (f WriteCloser, err error)
}

// Server - TFTP Server struct.
type Server struct {
	handler      ServerHandler
	readTimeout  time.Duration
	writeTimeout time.Duration
}

// NewServer - Create a new TFTP Server instance.
func NewServer(handler ServerHandler, readTimeout time.Duration, writeTimeout time.Duration) *Server {
	return &Server{
		handler:      handler,
		readTimeout:  readTimeout,
		writeTimeout: writeTimeout,
	}
}

// Run - TFTP server begin run. Addr is listen ip:port.
func (s *Server) Run(addr string) error {
	conn, err := net.ListenPacket("udp", addr)
	if err != nil {
		return err
	}
	for {
		if s.readTimeout != 0 {
			conn.SetReadDeadline(time.Now().Add(s.readTimeout))
		}

		if s.writeTimeout != 0 {
			conn.SetWriteDeadline(time.Now().Add(s.writeTimeout))
		}

		buff := make([]byte, 1024)
		n, raddr, err := conn.ReadFrom(buff)
		if err != nil || n == 0 {
			continue
		}
		buff = buff[:n]

		if peer, err := newClientPeer(raddr, s.handler, s.readTimeout, s.writeTimeout); err == nil {
			go peer.run(buff)
		}
	}
	panic("gotftp:can't reached!")
}
