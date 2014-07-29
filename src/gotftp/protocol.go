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
	errInvalidReq     = errors.New("invalid request")
	errBlockSizeOpt   = errors.New("invalid blocksize opt")
	errTimeoutOpt     = errors.New("invalid timeout value opt")
	errRolloverOpt    = errors.New("invalid rollover opt")
	errWriteBuffSmall = errors.New("write to buff failed, buffer too small")
	errUnknownType    = errors.New("unknown type")
)

type packet interface {
	getOpcode() uint16
	Read(buff *bytes.Buffer) error
	Write(buff *bytes.Buffer) error
}

type rwPacket struct {
	opcode          uint16
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

func (p *rwPacket) getOpcode() uint16 {
	return p.opcode
}

func (p *rwPacket) Read(buff *bytes.Buffer) error {
	var err error
	if p.fileName, err = buff.ReadString(0); err != nil {
		return err
	}
	p.fileName = p.fileName[:len(p.fileName)-1]
	if p.transferMode, err = buff.ReadString(0); err != nil {
		return err
	}
	p.transferMode = p.transferMode[:len(p.transferMode)-1]

	if buff.Len() != 0 {
		for buff.Len() > 0 {
			var optionName string
			if optionName, err = buff.ReadString(0); err != nil {
				return err
			}
			optionName = optionName[:len(optionName)-1]
			var value string
			if value, err = buff.ReadString(0); err != nil {
				return err
			}
			value = value[:len(value)-1]
			switch optionName {
			case blockSizeOptName:
				{
					var size int
					if size, err = strconv.Atoi(value); err != nil {
						return err
					}
					if size < int(minBlockSize) {
						return errBlockSizeOpt
					}
					if size > int(maxBlockSize) {
						size = int(maxBlockSize)
					}
					p.blockSize = uint16(size)
					p.hasBlockSize = true
				}
			case timeoutOptName:
				{
					var timeout int
					if timeout, err = strconv.Atoi(value); err != nil {
						return err
					}
					if uint16(timeout) < minTimeout || uint16(timeout) > maxTimeout {
						return errTimeoutOpt
					}
					p.timeout = time.Duration(timeout) * time.Second
					p.hasTimeout = true
				}
			case transferSizeOptName:
				{
					var size int
					if size, err = strconv.Atoi(value); err != nil {
						return err
					}
					p.transferSize = size
					p.hasTransferSize = true
				}
			default:
			}

		}
	}
	return nil
}

func (p *rwPacket) Write(buff *bytes.Buffer) error {
	if err := binary.Write(buff, binary.BigEndian, p.opcode); err != nil {
		return err
	}
	if _, err := buff.WriteString(p.fileName); err != nil {
		return err
	}
	if err := buff.WriteByte(0); err != nil {
		return err
	}
	if n, err := buff.WriteString(p.transferMode); err != nil {
		return err
	} else if n < len(p.transferMode) {
		return errWriteBuffSmall
	}
	return buff.WriteByte(0)
}

type dataPacket struct {
	blockID uint16
	data    []byte // max length is 512.
}

func (p *dataPacket) getOpcode() uint16 {
	return dataReqOp
}

func (p *dataPacket) Read(buff *bytes.Buffer) error {
	if err := binary.Read(buff, binary.BigEndian, &p.blockID); err != nil {
		return err
	}
	p.data = make([]byte, buff.Len())
	if _, err := buff.Read(p.data); err != nil {
		return err
	}
	return nil
}

func (p *dataPacket) Write(buff *bytes.Buffer) error {
	if err := binary.Write(buff, binary.BigEndian, dataReqOp); err != nil {
		return err
	}
	if err := binary.Write(buff, binary.BigEndian, p.blockID); err != nil {
		return err
	}
	if n, err := buff.Write(p.data); err != nil {
		return err
	} else if n < len(p.data) {
		return errWriteBuffSmall
	}
	return nil
}

type ackPacket struct {
	blockID uint16
}

func (p *ackPacket) getOpcode() uint16 {
	return ackReqOp
}

func (p *ackPacket) Read(buff *bytes.Buffer) error {
	return binary.Read(buff, binary.BigEndian, &p.blockID)
}

func (p *ackPacket) Write(buff *bytes.Buffer) error {
	if err := binary.Write(buff, binary.BigEndian, ackReqOp); err != nil {
		return err
	}
	return binary.Write(buff, binary.BigEndian, p.blockID)
}

type oackOption struct {
	name  string
	value string
}

type oackPacket struct {
	options []oackOption
}

func (p *oackPacket) getOpcode() uint16 {
	return oackReqOp
}

func (p *oackPacket) Read(buff *bytes.Buffer) error {
	for buff.Len() > 0 {
		var err error
		var opt oackOption
		if opt.name, err = buff.ReadString(0); err != nil {
			return err
		}
		opt.name = opt.name[:len(opt.name)-1]
		if opt.value, err = buff.ReadString(0); err != nil {
			return err
		}
		opt.value = opt.value[:len(opt.value)-1]
		p.options = append(p.options, opt)
	}
	return nil
}

func (p *oackPacket) Write(buff *bytes.Buffer) error {
	if err := binary.Write(buff, binary.BigEndian, oackReqOp); err != nil {
		return err
	}
	for _, v := range p.options {
		if n, err := buff.WriteString(v.name); err != nil {
			return err
		} else if n < len(v.name) {
			return errWriteBuffSmall
		}
		if err := buff.WriteByte(0); err != nil {
			return err
		}
		if n, err := buff.WriteString(v.value); err != nil {
			return err
		} else if n < len(v.value) {
			return errWriteBuffSmall
		}
		if err := buff.WriteByte(0); err != nil {
			return err
		}
	}
	return nil
}

type errorPacket struct {
	code uint32
	msg  string
}

func (p *errorPacket) getOpcode() uint16 {
	return errorReqOp
}

func (p *errorPacket) Read(buff *bytes.Buffer) error {
	var err error
	if err = binary.Read(buff, binary.BigEndian, &p.code); err != nil {
		return err
	}
	if p.msg, err = buff.ReadString(0); err != nil {
		return err
	}
	p.msg = p.msg[:len(p.msg)-1]
	return nil
}

func (p *errorPacket) Write(buff *bytes.Buffer) error {
	if err := binary.Write(buff, binary.BigEndian, errorReqOp); err != nil {
		return err
	}
	if err := binary.Write(buff, binary.BigEndian, p.code); err != nil {
		return err
	}
	if n, err := buff.WriteString(p.msg); err != nil {
		return err
	} else if n < len(p.msg) {
		return errWriteBuffSmall
	}
	return buff.WriteByte(0)
}

func getRequestPacket(data []byte) (packet interface{}, err error) {
	if len(data) < 2 {
		return nil, errInvalidReq
	}
	buff := bytes.NewBuffer(data)
	var opcode uint16
	if err = binary.Read(buff, binary.BigEndian, &opcode); err != nil {
		return nil, err
	}
	switch opcode {
	case readFileReqOp:
		{
			p := &rwPacket{opcode: readFileReqOp}
			if err = p.Read(buff); err != nil {
				return nil, err
			}
			if len(p.fileName) == 0 || len(p.transferMode) == 0 ||
				(p.transferMode != netASCIIMode && p.transferMode != binaryMode) {
				return nil, err
			}
			return p, nil
		}
	case writeFileReqOp:
		{
			p := &rwPacket{opcode: writeFileReqOp}
			if err = p.Read(buff); err != nil {
				return nil, err
			}

			if len(p.fileName) == 0 || len(p.transferMode) == 0 ||
				(p.transferMode != netASCIIMode && p.transferMode != binaryMode) {
				return nil, errInvalidReq
			}
			return p, nil
		}
	case dataReqOp:
		{
			p := &dataPacket{}
			if err = p.Read(buff); err != nil {
				return nil, err
			}
			return p, nil
		}
	case ackReqOp:
		{
			p := &ackPacket{}
			if err = p.Read(buff); err != nil {
				return nil, err
			}
			return p, nil
		}
	case oackReqOp:
		{
			p := &oackPacket{}
			if err = p.Read(buff); err != nil {
				return nil, err
			}
			return p, nil
		}
	case errorReqOp:
		{
			p := &errorPacket{}
			if err = p.Read(buff); err != nil {
				return nil, err
			}
			return p, nil
		}
	}
	return nil, errInvalidReq
}

func packetReq(req interface{}) []byte {
	if packet, ok := req.(packet); ok {
		buff := bytes.NewBuffer(nil)
		if err := packet.Write(buff); err == nil {
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
	var eq errorPacket
	eq.code = 0
	eq.msg = err
	b := packetReq(eq)
	if b != nil {
		conn.WriteTo(b, addr)
	}
}

func getResponse(conn net.PacketConn, readTimeout, writeTimeout time.Duration) (resp interface{}, raddr net.Addr, err error) {
	if readTimeout != 0 {
		conn.SetReadDeadline(time.Now().Add(readTimeout))
	}
	if writeTimeout != 0 {
		conn.SetWriteDeadline(time.Now().Add(writeTimeout))
	}
	b := make([]byte, 2048)
	var n int
	n, raddr, err = conn.ReadFrom(b)
	if err != nil {
		return nil, raddr, err
	}
	resp, err = getRequestPacket(b[:n])
	return
}

func processResponse(conn net.PacketConn, readTimeout, writeTimeout time.Duration,
	raddr *net.Addr, processor func(resp interface{}) (goon bool, err error)) error {
	for {
		var resp interface{}
		var err error
		var newAddr net.Addr
		if resp, newAddr, err = getResponse(conn, readTimeout, writeTimeout); err != nil {
			return err
		}
		if raddr != nil {
			*raddr = newAddr
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
		case *errorPacket:
			{
				return errors.New(t.msg)
			}
		}
	}
	return nil
}
