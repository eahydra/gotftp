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
	"bytes"
	"encoding/binary"
	"errors"
	"net"
	"strconv"
	"time"
)

const (
	netASCIIMode        = "netascii"
	binaryMode          = "octet"
	blockSizeOptName    = "blksize"
	timeoutOptName      = "timeout"
	transferSizeOptName = "tsize"

	defaultBlockSize uint16 = 512
	minBlockSize     uint16 = 8
	maxBlockSize     uint16 = 65464

	defaultTimeout uint16 = 3
	minTimeout     uint16 = 1
	maxTimeout     uint16 = 255

	maxTransferSize int = int(defaultBlockSize) * 65535

	readFileReqOp  uint16 = 1
	writeFileReqOp uint16 = 2
	dataReqOp      uint16 = 3
	ackReqOp       uint16 = 4
	errorReqOp     uint16 = 5
	oackReqOp      uint16 = 6
)

var (
	errInvalidReq   = errors.New("invalid request")
	errBlockSizeOpt = errors.New("invalid blocksize opt")
	errTimeoutOpt   = errors.New("invalid timeout value opt")
	errRolloverOpt  = errors.New("invalid rollover opt")
)

type (
	beginReq struct {
		fileName        string
		transferMode    string
		hasOption       bool
		hasBlockSize    bool
		blockSize       uint16
		hasTransferSize bool
		transferSize    int
		hasTimeout      bool
		timeout         time.Duration
	}
	readFileReq struct {
		beginReq
	}

	writeFileReq struct {
		beginReq
	}

	dataReq struct {
		blockID uint16
		data    []byte // max length is 512.
	}

	ackReq struct {
		blockID uint16
	}

	oackOpt struct {
		name  string
		value string
	}

	oackReq struct {
		opts []oackOpt
	}

	errorReq struct {
		code uint16
		msg  string
	}
)

type optionHandler struct {
	name    string
	handler func(value string, req *beginReq) error
}

var optHandlers = []optionHandler{
	{
		name: blockSizeOptName,
		handler: func(value string, req *beginReq) error {
			var size int
			var err error
			if size, err = strconv.Atoi(value); err != nil {
				return err
			}
			if size < int(minBlockSize) {
				return errBlockSizeOpt
			}
			if size > int(maxBlockSize) {
				size = int(maxBlockSize)
			}
			req.blockSize = uint16(size)
			req.hasBlockSize = true
			return nil
		},
	},
	{
		name: timeoutOptName,
		handler: func(value string, req *beginReq) error {
			var v int
			var err error
			if v, err = strconv.Atoi(value); err != nil {
				return err
			}
			if uint16(v) < minTimeout || uint16(v) > maxTimeout {
				return errTimeoutOpt
			}
			req.timeout = time.Duration(v) * time.Second
			req.hasTimeout = true
			return nil
		},
	},
	{
		name: transferSizeOptName,
		handler: func(value string, req *beginReq) error {
			var size int
			var err error
			if size, err = strconv.Atoi(value); err != nil {
				return err
			}
			req.transferSize = size
			req.hasTransferSize = true
			return nil
		},
	},
}

func getOption(buff []byte) (req beginReq, err error) {
	values := bytes.Split(buff, []byte{0})
	if len(values)-1 < 2 {
		err = errInvalidReq
		return
	}
	req.fileName = string(bytes.ToLower(values[0]))
	req.transferMode = string(bytes.ToLower(values[1]))
	if len(values)-1 == 2 {
		return
	}
	// there are some option such as blksize, timeout or another thing.
	if (len(values[2:])-1)%2 != 0 {
		err = errInvalidReq
		return
	}
	values = values[2:]
	for i := 0; i < len(values); {
		s := string(bytes.ToLower(values[i]))
		value := string(values[i+1])
		for _, v := range optHandlers {
			if v.name == s {
				if err = v.handler(value, &req); err != nil {
					return
				}
			}
		}
		i += 2
	}
	return
}

func getRequestPacket(buff []byte) (packet interface{}, err error) {
	if len(buff) < 2 {
		return nil, errInvalidReq
	}
	opcode := binary.BigEndian.Uint16(buff[0:2])
	switch opcode {
	case readFileReqOp:
		{
			var rrq readFileReq
			rrq.beginReq, err = getOption(buff[2:])
			if err != nil || len(rrq.fileName) == 0 || len(rrq.transferMode) == 0 ||
				(rrq.transferMode != netASCIIMode && rrq.transferMode != binaryMode) {
				return nil, errInvalidReq
			}
			return rrq, nil
		}
	case writeFileReqOp:
		{
			var wrq writeFileReq
			wrq.beginReq, err = getOption(buff[2:])
			if len(wrq.fileName) == 0 || len(wrq.transferMode) == 0 ||
				(wrq.transferMode != netASCIIMode && wrq.transferMode != binaryMode) {
				return nil, errInvalidReq
			}
			return wrq, nil
		}
	case dataReqOp:
		{
			if len(buff[2:]) < 2 {
				return nil, errInvalidReq
			}
			var dq dataReq
			dq.blockID = binary.BigEndian.Uint16(buff[2:])
			dq.data = make([]byte, len(buff[4:]))
			copy(dq.data, buff[4:])
			return dq, nil
		}
	case ackReqOp:
		{
			if len(buff[2:]) < 2 {
				return nil, errInvalidReq
			}
			var ackReq ackReq
			ackReq.blockID = binary.BigEndian.Uint16(buff[2:])
			return ackReq, nil
		}
	case oackReqOp:
		{
			opts := bytes.Split(buff[2:], []byte{0})
			if (len(opts)-1)%2 != 0 {
				return nil, errInvalidReq
			}
			var oack oackReq
			for i := 0; i < len(opts); {
				var opt oackOpt
				opt.name = string(bytes.ToLower(opts[i]))
				opt.value = string(opts[i+1])
				oack.opts = append(oack.opts, opt)
			}
			return oack, nil
		}
	case errorReqOp:
		{
			if len(buff[2:]) < 2 {
				return nil, errInvalidReq
			}
			var err errorReq
			err.code = binary.BigEndian.Uint16(buff[2:])
			var eof bool
			var msg []byte
			for _, v := range buff[4:] {
				if v != 0 {
					msg = append(msg, v)
				} else {
					eof = true
					break
				}
			}
			if !eof {
				return nil, errInvalidReq
			}
			return err, nil
		}
	}
	return nil, errInvalidReq
}

func packetReq(req interface{}) []byte {
	switch t := req.(type) {
	case readFileReq:
		{
			buff := new(bytes.Buffer)
			binary.Write(buff, binary.BigEndian, readFileReqOp)
			buff.WriteString(t.fileName)
			buff.WriteByte(0)
			buff.WriteString(t.transferMode)
			buff.WriteByte(0)
			return buff.Bytes()
		}
	case writeFileReq:
		{
			buff := new(bytes.Buffer)
			binary.Write(buff, binary.BigEndian, writeFileReqOp)
			buff.WriteString(t.fileName)
			buff.WriteByte(0)
			buff.WriteString(t.transferMode)
			buff.WriteByte(0)
			return buff.Bytes()
		}
	case dataReq:
		{
			buff := new(bytes.Buffer)
			binary.Write(buff, binary.BigEndian, dataReqOp)
			binary.Write(buff, binary.BigEndian, t.blockID)
			buff.Write(t.data)
			return buff.Bytes()
		}
	case ackReq:
		{
			buff := new(bytes.Buffer)
			binary.Write(buff, binary.BigEndian, ackReqOp)
			binary.Write(buff, binary.BigEndian, t.blockID)
			return buff.Bytes()
		}
	case oackReq:
		{
			buff := new(bytes.Buffer)
			binary.Write(buff, binary.BigEndian, oackReqOp)
			for _, v := range t.opts {
				buff.WriteString(v.name)
				buff.WriteByte(0)
				buff.WriteString(v.value)
				buff.WriteByte(0)
			}
			return buff.Bytes()
		}
	case errorReq:
		{
			buff := new(bytes.Buffer)
			binary.Write(buff, binary.BigEndian, errorReqOp)
			binary.Write(buff, binary.BigEndian, t.code)
			buff.WriteString(t.msg)
			buff.WriteByte(0)
			return buff.Bytes()
		}
	}
	return nil
}

func sendPacket(conn net.PacketConn, addr net.Addr, p interface{}) error {
	if b := packetReq(p); b != nil {
		if _, err := conn.WriteTo(b, addr); err != nil {
			return err
		}
	} else {
		err := errors.New("packet req failed")
		sendErrorReq(conn, addr, err.Error())
		return err
	}
	return nil
}

func sendErrorReq(conn net.PacketConn, addr net.Addr, err string) {
	var eq errorReq
	eq.code = 0
	eq.msg = err
	b := packetReq(eq)
	if b != nil {
		conn.WriteTo(b, addr)
	}
}

func getResponse(conn net.PacketConn, readTimeout, writeTimeout time.Duration) (resp interface{}, err error) {
	if readTimeout != 0 {
		conn.SetReadDeadline(time.Now().Add(readTimeout))
	}
	if writeTimeout != 0 {
		conn.SetWriteDeadline(time.Now().Add(writeTimeout))
	}
	b := make([]byte, 2048)
	n, _, err := conn.ReadFrom(b)
	if err != nil {
		return nil, err
	}
	return getRequestPacket(b[:n])
}

func processResponse(conn net.PacketConn, readTimeout, writeTimeout time.Duration,
	processor func(resp interface{}) (goon bool, err error)) error {
	for {
		var resp interface{}
		var err error
		if resp, err = getResponse(conn, readTimeout, writeTimeout); err != nil {
			return err
		}
		switch t := resp.(type) {
		default:
			{
				var goon bool
				if goon, err = processor(resp); err != nil {
					return err
				} else if !goon {
					return nil
				}
			}
		case errorReq:
			{
				return errors.New(t.msg)
			}
		}
	}
	return nil
}
