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
	"errors"
	"io"
	"net"
	"strconv"
	"time"
)

type clientPeer struct {
	conn         net.PacketConn
	addr         net.Addr
	handler      ServerHandler
	readTimeout  time.Duration
	writeTimeout time.Duration
	blockSize    uint16
	fileSize     int
}

func newClientPeer(raddr net.Addr, handler ServerHandler, readTimout, writeTimeout time.Duration) (p *clientPeer, err error) {
	conn, err := net.ListenPacket("udp", ":0")
	if err != nil {
		return nil, err
	}
	p = new(clientPeer)
	p.conn = conn
	p.addr = raddr
	p.handler = handler
	p.readTimeout = readTimout
	p.writeTimeout = writeTimeout
	p.blockSize = defaultBlockSize
	p.fileSize = 0
	return
}

func (peer *clientPeer) close() {
	peer.conn.Close()
}

func (peer *clientPeer) run(data []byte) {
	defer peer.close()

	req, err := getRequestPacket(data)
	if err != nil {
		sendErrorReq(peer.conn, peer.addr, err.Error())
		return
	}
	if t, ok := req.(readFileReq); ok {
		err = peer.handleRRQ(t)
	} else if t, ok := req.(writeFileReq); ok {
		err = peer.handleWRQ(t)
	}
	if err != nil {
		logln("err:", err.Error())
	}
	return
}

func (peer *clientPeer) applyBlockSizeOpt(req beginReq) (opt *oackOpt, err error) {
	if req.hasBlockSize {
		peer.blockSize = defaultBlockSize
		if req.blockSize < defaultBlockSize {
			peer.blockSize = req.blockSize
		}
		var opt oackOpt
		opt.name = blockSizeOptName
		opt.value = strconv.Itoa(int(peer.blockSize))
		logf("process blocksize opt <blockSize=%dbyte>", peer.blockSize)
		return &opt, nil
	}
	return
}

func (peer *clientPeer) applyTimeoutOpt(req beginReq) (opt *oackOpt, err error) {
	if req.hasTimeout {
		peer.readTimeout, peer.writeTimeout = req.timeout, req.timeout
		var opt oackOpt
		opt.name = timeoutOptName
		opt.value = strconv.Itoa(int(req.timeout.Seconds()))
		logf("process timeout opt <timeout=%s>", peer.readTimeout.String())
		return &opt, nil
	}
	return
}

func (peer *clientPeer) applyTransferSizeOpt(req beginReq) (opt *oackOpt, err error) {
	if req.hasTransferSize {
		var opt oackOpt
		opt.name = transferSizeOptName
		if req.transferSize == 0 {
			opt.value = strconv.Itoa(peer.fileSize)
			logf("process tsize opt <orgTsize=0, newTsize=%d>", peer.fileSize)
		} else {
			if req.transferSize > maxTransferSize {
				logf("process tsize opt <tisze=%d> is too big", req.transferSize)
				return nil, errors.New("transferSize is too big")
			}
			peer.fileSize = req.transferSize
			opt.value = strconv.Itoa(int(req.transferSize))
			logf("process tsize opt <tsize=%d>", req.transferSize)
		}
		return &opt, nil
	}
	return
}

func (peer *clientPeer) applyOptions(req beginReq) (ackOpts []oackOpt, err error) {
	applier := []func(req beginReq) (opt *oackOpt, err error){
		peer.applyBlockSizeOpt,
		peer.applyTimeoutOpt,
		peer.applyTransferSizeOpt,
	}
	for _, v := range applier {
		var opt *oackOpt
		if opt, err = v(req); err != nil {
			return nil, err
		}
		if opt != nil {
			ackOpts = append(ackOpts, *opt)
		}
	}
	return
}

func (peer *clientPeer) handleRRQNegotiation(req readFileReq) (err error) {
	if req.hasOption {
		logf("begin RRQ Negotiation")
		defer func() {
			if err == nil {
				logf("end RRQ Negotiation success")
			} else {
				logf("end RRQ Negotiation failed. err=%s", err.Error())
			}
		}()

		var opts []oackOpt
		if opts, err = peer.applyOptions(req.beginReq); err != nil {
			sendErrorReq(peer.conn, peer.addr, err.Error())
			return err
		}
		var oack oackReq
		oack.opts = opts
		if err = sendPacket(peer.conn, peer.addr, oack); err != nil {
			logf("send OACK failed. err=%s", err.Error())
			return err
		}
		logf("send OACK")

		return processResponse(peer.conn, peer.readTimeout, peer.writeTimeout, nil,
			func(resp interface{}) (goon bool, err error) {
				if ack, ok := resp.(ackReq); ok {
					if ack.blockID == 0 {
						logf("recv ACK <blockID=0>")
						return false, nil
					}
				}
				return true, nil
			})
	}
	return nil
}

func (peer *clientPeer) handleRRQ(req readFileReq) error {
	logf("begin RRQ <fileName=%s, mode=%s, from=%s>", req.fileName, req.transferMode, peer.addr.String())
	defer logf("end RRQ")

	rc, err := peer.handler.ReadFile(req.fileName)
	if err != nil {
		logf("Open File Failed. err=%s", err.Error())
		sendErrorReq(peer.conn, peer.addr, err.Error())
		return err
	}
	defer rc.Close()
	logf("Open File Success")

	var fileSize int64
	if fileSize, err = rc.Size(); err != nil {
		sendErrorReq(peer.conn, peer.addr, err.Error())
		return err
	}
	peer.fileSize = int(fileSize)

	if err = peer.handleRRQNegotiation(req); err != nil {
		return err
	}

	buff := make([]byte, peer.blockSize)
	var blockID uint16 = 1
	for {
		n, err := rc.Read(buff)
		if err != nil {
			if err != io.EOF {
				logf("readFile failed. err=%s", err.Error())
				sendErrorReq(peer.conn, peer.addr, err.Error())
				return err
			}
		}

		var dq dataReq
		dq.blockID = blockID
		dq.data = buff[0:n]
		if err = sendPacket(peer.conn, peer.addr, dq); err != nil {
			logf("send DQ failed. err=%s <blockID=%d, %dbytes>", err.Error(), blockID, len(dq.data))
			return err
		}
		logf("send DQ  <blockID=%d, %dbytes>", blockID, len(dq.data))

		err = processResponse(peer.conn, peer.readTimeout, peer.writeTimeout, nil,
			func(resp interface{}) (goon bool, err error) {
				if ack, ok := resp.(ackReq); ok {
					if ack.blockID == blockID {
						return false, nil
					}
				}
				return true, nil
			})
		if err != nil {
			logf("recv ACK failed. err=%s <blockID=%d>", err.Error(), blockID)
			return err
		}
		logf("recv ACK <blockID=%d>", blockID)
		if n < int(peer.blockSize) {
			logf("finalACK")
			break
		}
		blockID++
	}

	return nil
}

func (peer *clientPeer) handleWRQNegotiation(req writeFileReq) (err error) {
	if req.hasOption {
		logf("begin WRQ Negotiation")
		defer func() {
			if err == nil {
				logf("end WRQ Negotiation success")
			} else {
				logf("end WRQ Negotiation failed. err=%s", err.Error())
			}
		}()

		var opts []oackOpt
		if opts, err = peer.applyOptions(req.beginReq); err != nil {
			sendErrorReq(peer.conn, peer.addr, err.Error())
			return err
		}
		var oack oackReq
		oack.opts = opts
		if err = sendPacket(peer.conn, peer.addr, oack); err != nil {
			logf("send OACK failed.err=%s", err.Error())
			return err
		}
		logf("send OACK")
	} else {
		var ack ackReq
		ack.blockID = 0
		if err = sendPacket(peer.conn, peer.addr, ack); err != nil {
			logf("send ACK failed, err=%s, <blockID=0>", err.Error())
			return err
		}
		logf("send ACK <blockID=0>")
	}
	return nil
}

func (peer *clientPeer) handleWRQ(req writeFileReq) error {
	logf("begin WRQ <fileName=%s, mode=%s, from=%s>", req.fileName, req.transferMode, peer.addr.String())
	defer logf("end WRQ")

	wc, err := peer.handler.WriteFile(req.fileName)
	if err != nil {
		logf("Open File Failed. err=%s", err.Error())
		sendErrorReq(peer.conn, peer.addr, err.Error())
		return err
	}
	defer wc.Close()
	logf("Open File success")

	if err = peer.handleWRQNegotiation(req); err != nil {
		return err
	}

	var blockID uint16 = 1
	var finalACK bool
	var transferSize int
	if peer.fileSize == 0 {
		peer.fileSize = maxTransferSize
	}
	for transferSize < peer.fileSize {
		err = processResponse(peer.conn, peer.readTimeout, peer.writeTimeout, nil,
			func(resp interface{}) (goon bool, err error) {
				if dq, ok := resp.(dataReq); ok {
					if dq.blockID != blockID {
						return true, nil
					}
					logf("recv DQ  <blockID=%d, %dbytes>", blockID, len(dq.data))
					if _, err := wc.Write(dq.data); err != nil {
						logf("write failed. err=%s", err.Error())
						sendErrorReq(peer.conn, peer.addr, err.Error())
						return false, err
					}
					if len(dq.data) < int(peer.blockSize) {
						finalACK = true
					}
					transferSize += len(dq.data)
					return false, nil
				}
				return true, nil
			})
		if err != nil {
			logf("recv DQ failed. err=%s <blockID=%d>", err.Error(), blockID)
			return err
		}

		var ack ackReq
		ack.blockID = blockID
		if err = sendPacket(peer.conn, peer.addr, ack); err != nil {
			logf("send ACK failed. err=%s <blockID=%d>", err.Error(), blockID)
			return err
		}
		logf("send ACK <blockID=%d>", blockID)

		if finalACK {
			logf("finalACK")
			processResponse(peer.conn, peer.readTimeout, peer.writeTimeout, nil,
				func(resp interface{}) (goon bool, err error) {
					// if recv dq, means final ack was lost,
					// so if blockID matched, then resend final ack
					if dq, ok := resp.(dataReq); ok {
						if dq.blockID == blockID {
							sendPacket(peer.conn, peer.addr, ack)
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
