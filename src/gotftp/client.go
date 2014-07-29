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

// ReadFile - read filename from addr to w. The MAX size is 32MB
func ReadFile(addr string, filename string, w io.Writer) error {
	logf("begin RRQ <fileName=%s mode=octet to=%s>", filename, addr)
	defer logf("end RRQ")

	conn, err := net.ListenPacket("udp", ":0")
	if err != nil {
		logf("open connection failed. err=%s", err.Error())
		return err
	}
	defer conn.Close()
	logf("open connection success")

	var raddr net.Addr
	raddr, err = net.ResolveUDPAddr("udp", addr)
	if err != nil {
		logf("resolve address failed. err=%s", err.Error())
		return err
	}

	rrq := &rwPacket{opcode: readFileReqOp}
	rrq.fileName = filename
	rrq.transferMode = binaryMode
	if err = sendPacket(conn, raddr, rrq); err != nil {
		logf("send RRQ failed. err=%s", err.Error())
		return err
	}
	logf("send RRQ <filename=%s, mode=octet>", filename)

	var blockID uint16 = 1
	var finalACK bool
	readTimeout := time.Duration(3) * time.Second
	writeTimeout := readTimeout
	for {
		err = processResponse(conn, readTimeout, writeTimeout, &raddr,
			func(resp interface{}) (goon bool, err error) {
				if dq, ok := resp.(*dataPacket); ok {
					if dq.blockID != blockID {
						return true, nil
					}
					logf("recv DQ  <blockID=%d %dbytes>", blockID, len(dq.data))
					if _, err = w.Write(dq.data); err != nil {
						logf("wrtie failed. err=%s", err.Error())
						sendErrorReq(conn, raddr, err.Error())
						return false, err
					}
					if len(dq.data) != int(defaultBlockSize) {
						finalACK = true
					}
					return false, nil

				}
				return true, nil
			})
		if err != nil {
			return err
		}

		ack := &ackPacket{}
		ack.blockID = blockID
		if err = sendPacket(conn, raddr, ack); err != nil {
			logf("send ACK failed. err=%s <blockID=%d>", err.Error(), blockID)
			return err
		}
		logf("send ACK <blockID=%d>", blockID)

		if finalACK {
			logf("finalACk")
			processResponse(conn, readTimeout, writeTimeout, &raddr,
				func(resp interface{}) (goon bool, err error) {
					if dq, ok := resp.(*dataPacket); ok {
						if dq.blockID == blockID {
							sendPacket(conn, raddr, ack)
						}
					}
					return false, nil
				})
			break
		}
		blockID++
	}
	return nil
}

// WriteFile - Write fileName from reader to addr. The max size is 32MB
func WriteFile(addr string, fileName string, reader io.Reader) error {
	logf("begin WRQ <filename=%s mode=octet to=%s>", fileName, addr)
	defer logf("end WRQ")

	conn, err := net.ListenPacket("udp4", ":0")
	if err != nil {
		logf("open connection failed. err=%s", err.Error())
		return err
	}
	defer conn.Close()
	logf("open connection success")

	var raddr net.Addr
	raddr, err = net.ResolveUDPAddr("udp", addr)
	if err != nil {
		logf("resolve address failed. err=%s", err.Error())
		return err
	}

	wrq := &rwPacket{opcode: writeFileReqOp}
	wrq.fileName = fileName
	wrq.transferMode = binaryMode
	if err = sendPacket(conn, raddr, wrq); err != nil {
		logf("send WRQ failed. err=%s <filename=%s mode=octet>", err.Error(), fileName)
		return err
	}
	logf("send WRQ <filename=%s> mode=octet", fileName)

	readTimeout := time.Duration(3) * time.Second
	writeTimeout := readTimeout
	err = processResponse(conn, readTimeout, writeTimeout, &raddr,
		func(resp interface{}) (goon bool, err error) {
			if ack, ok := resp.(*ackPacket); ok {
				if ack.blockID == 0 {
					return false, nil
				}
			}
			return true, nil
		})
	if err != nil {
		logf("recv ACK failed. err=%s <blockID=0>", err.Error())
		return err
	}
	logf("recv ACK <blockID=0>")

	var blockID uint16 = 1
	for {
		b := make([]byte, defaultBlockSize)
		var n int
		if n, err = reader.Read(b); err != nil {
			if err != io.EOF {
				logf("read failed. err=%s", err.Error())
				sendErrorReq(conn, raddr, err.Error())
				return err
			}
		}

		dq := &dataPacket{}
		dq.blockID = blockID
		dq.data = b[:n]
		if err = sendPacket(conn, raddr, dq); err != nil {
			logf("send DQ failed. err=%s, <blockID=%d %dbytes>", err.Error(), blockID, len(dq.data))
			return err
		}
		logf("send DQ  <blockID=%d %dbytes>", blockID, len(dq.data))

		err = processResponse(conn, readTimeout, writeTimeout, &raddr,
			func(resp interface{}) (goon bool, err error) {
				if ack, ok := resp.(*ackPacket); ok {
					if ack.blockID == blockID {
						return false, nil
					}
				}
				return true, nil
			})
		if err != nil {
			logf("recv ACK failed. err=%s, <blockID=%d>", err.Error(), blockID)
			return err
		}
		logf("recv ACK <blockID=%d>", blockID)

		if n < int(defaultBlockSize) {
			break
		}
		blockID++
	}
	return nil
}
