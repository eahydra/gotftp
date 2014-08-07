package gotftp

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"
)

var (
	errNotDefinedPacket       = []byte{0x00, 0x05, 0x00, 0x00}
	errFileNotFoundPacket     = []byte{0x00, 0x05, 0x00, 0x01, 0x00}
	errIllegalOperationPacket = []byte{0x00, 0x05, 0x00, 0x04, 0x00}
	errFileExistPacket        = []byte{0x00, 0x05, 0x00, 0x06, 0x00}
)

func handleError(buff *bytes.Buffer) error {
	var code uint16
	var err error
	if err = binary.Read(buff, binary.BigEndian, &code); err == nil {
		var msg string
		msg, err = buff.ReadString(0)
		if err != nil {
			switch code {
			case 1:
				msg = "file not found"
			case 2:
				msg = "access violation"
			case 3:
				msg = "disk full or allocation exceeded"
			case 4:
				msg = "illegal tftp operation"
			case 5:
				msg = "unknown transfer id"
			case 6:
				msg = "file already exists"
			case 7:
				msg = "no such user"
			default:
				msg = "not define"
			}
		}
		err = fmt.Errorf("code:%d, msg:%s", code, msg)
	}
	return err
}

func sendError(conn net.PacketConn, remoteAddr net.Addr, err error) {
	logln("err:", err)
	errPacket := bytes.NewBuffer(nil)
	errPacket.Write(errNotDefinedPacket)
	errPacket.WriteString(err.Error())
	errPacket.WriteByte(0)
	conn.WriteTo(errPacket.Bytes(), remoteAddr)
}

func sendOptionAck(conn net.PacketConn, remoteAddr net.Addr, blkSize, timeout, tsize int) {
	oack := bytes.NewBuffer([]byte{0x00, 0x06})
	oack.WriteString("blksize")
	oack.WriteByte(0)
	oack.WriteString(fmt.Sprintf("%d", blkSize))
	oack.WriteByte(0)
	oack.WriteString("timeout")
	oack.WriteByte(0)
	oack.WriteString(fmt.Sprintf("%d", timeout))
	oack.WriteByte(0)
	oack.WriteString("tsize")
	oack.WriteByte(0)
	oack.WriteString(fmt.Sprintf("%d", tsize))
	oack.WriteByte(0)
	conn.WriteTo(oack.Bytes(), remoteAddr)
}

type clientPeer struct {
	remoteAddr      net.Addr
	keepaliveTime   time.Time
	blockSize       int
	transferSize    int
	timeout         int
	fileHandler     FileHandler
	readSeekCloser  ReadSeekCloser
	writeSeekCloser WriteSeekCloser
}

func newClientPeer(remoteAddr net.Addr, fileHandler FileHandler) *clientPeer {
	return &clientPeer{
		remoteAddr:    remoteAddr,
		keepaliveTime: time.Now(),
		blockSize:     512,
		timeout:       10,
		fileHandler:   fileHandler,
	}
}

func (p *clientPeer) Close() error {
	if p.readSeekCloser != nil {
		p.readSeekCloser.Close()
	}
	if p.writeSeekCloser != nil {
		p.writeSeekCloser.Close()
	}
	return nil
}

func (p *clientPeer) Dispatch(conn net.PacketConn, data []byte) {
	p.keepaliveTime = time.Now()

	buff := bytes.NewBuffer(data)
	var operation uint16
	if err := binary.Read(buff, binary.BigEndian, &operation); err != nil {
		// it's a invalid packet. just do nothing.
		return
	}

	switch operation {
	case 1: // read request
		p.HandleReadHandshake(conn, buff)
	case 2: // write request
		p.HandleWriteHandshake(conn, buff)
	case 3: // data packet
		p.HandleWriteData(conn, buff)
	case 4: // ack packet
		p.HandleReadAck(conn, buff)
	case 5: // error packet
		{
			if err := handleError(buff); err != nil {
				logln("err:", err)
			}
		}
	}
}

func (p *clientPeer) HandleReadHandshake(conn net.PacketConn, buff *bytes.Buffer) {
	var fileName string
	var err error
	if fileName, err = buff.ReadString(0); err != nil {
		conn.WriteTo(errIllegalOperationPacket, p.remoteAddr)
		return
	}
	fileName = fileName[:len(fileName)-1]

	if exist, err := p.fileHandler.IsFileExist(p.remoteAddr.String(), fileName); err != nil || !exist {
		if err != nil {
			sendError(conn, p.remoteAddr, err)
			return
		}
		conn.WriteTo(errFileNotFoundPacket, p.remoteAddr)
		return
	}

	if p.readSeekCloser == nil {
		if p.readSeekCloser, err = p.fileHandler.ReadFile(p.remoteAddr.String(), fileName); err != nil {
			sendError(conn, p.remoteAddr, err)
			return
		}
	}

	var mode string
	if mode, err = buff.ReadString(0); err != nil {
		conn.WriteTo(errIllegalOperationPacket, p.remoteAddr)
		return
	}
	mode = strings.ToLower(mode[:len(mode)-1])
	if mode != "octet" {
		conn.WriteTo(errIllegalOperationPacket, p.remoteAddr)
		return
	}

	hasOption := false
	if buff.Len() > 0 {
		hasOption = true
	}
	for buff.Len() > 0 {
		var optionName string
		if optionName, err = buff.ReadString(0); err != nil {
			break
		}
		optionName = strings.ToLower(optionName[:len(optionName)-1])
		var value string
		if value, err = buff.ReadString(0); err != nil {
			break
		}
		value = value[:len(value)-1]
		switch optionName {
		case "blksize":
			{
				var size int
				if size, err = strconv.Atoi(value); err != nil {
					break
				}
				// RFC2348 define the minimum size is 8byte
				if size < 8 {
					err = fmt.Errorf("the value of blksize is too small")
					break
				}

				if size < p.blockSize {
					p.blockSize = size
				}
			}
		case "timeout":
			{
				var timeout int
				if timeout, err = strconv.Atoi(value); err != nil {
					break
				}
				// RFC2349 define the minimum timeout is 1second.
				if timeout < 1 {
					err = fmt.Errorf("the value of timeout is invalid")
					break
				}
				if timeout < p.timeout {
					p.timeout = timeout
				}
			}
		case "tsize":
			{
				tsize, err := p.readSeekCloser.Seek(0, 2)
				if err != nil {
					break
				}
				p.transferSize = int(tsize)
			}
		default:
			{
				err = fmt.Errorf("unknown option")
				break
			}
		}
		if err != nil {
			break
		}
	}

	if err != nil {
		sendError(conn, p.remoteAddr, err)
		return
	}

	if hasOption {
		sendOptionAck(conn, p.remoteAddr, p.blockSize, p.timeout, p.transferSize)
	} else {
		datapacket := make([]byte, 4+p.blockSize)
		n, err := p.readSeekCloser.Read(datapacket[4:])
		if err != nil {
			sendError(conn, p.remoteAddr, err)
		} else {
			binary.BigEndian.PutUint16(datapacket, 0x03)
			binary.BigEndian.PutUint16(datapacket[2:], 0x01)
			conn.WriteTo(datapacket[:n+4], p.remoteAddr)
			logln("send datapacket, blockid: 1")
		}
	}
	return
}

func (p *clientPeer) HandleReadAck(conn net.PacketConn, buff *bytes.Buffer) {
	if p.readSeekCloser == nil {
		conn.WriteTo(errIllegalOperationPacket, p.remoteAddr)
		return
	}

	var blockID uint16
	if err := binary.Read(buff, binary.BigEndian, &blockID); err != nil {
		logln("HandleReadAck parse block failed, err:", err)
		return
	}
	logln("get ack, blockid:", blockID)

	fileSize, err := p.readSeekCloser.Seek(0, 2)
	if err != nil {
		sendError(conn, p.remoteAddr, err)
		return
	}

	if fileSize < int64(blockID)*int64(p.blockSize) {
		return
	}

	datapacket := make([]byte, 4+p.blockSize)
	binary.BigEndian.PutUint16(datapacket, 0x03)
	binary.BigEndian.PutUint16(datapacket[2:], blockID+1)
	if _, err := p.readSeekCloser.Seek(int64(blockID)*int64(p.blockSize), 0); err != nil {
		sendError(conn, p.remoteAddr, err)
		return
	}
	n, err := p.readSeekCloser.Read(datapacket[4:])
	if err != nil && err != io.EOF {
		sendError(conn, p.remoteAddr, err)
		return
	}

	_, err = conn.WriteTo(datapacket[:4+n], p.remoteAddr)
	if err != nil {
		logln("DQ err:", err)
	}
	if n < p.blockSize {
		logln("Final DQ, n:", n)
	}
	return
}

func (p *clientPeer) HandleWriteHandshake(conn net.PacketConn, buff *bytes.Buffer) {
	var fileName string
	var err error
	if fileName, err = buff.ReadString(0); err != nil {
		conn.WriteTo(errIllegalOperationPacket, p.remoteAddr)
		return
	}
	fileName = fileName[:len(fileName)-1]

	if exist, err := p.fileHandler.IsFileExist(p.remoteAddr.String(), fileName); err != nil || exist {
		if err != nil {
			sendError(conn, p.remoteAddr, err)
			return
		}
		conn.WriteTo(errFileExistPacket, p.remoteAddr)
		return
	}
	if p.writeSeekCloser == nil {
		if p.writeSeekCloser, err = p.fileHandler.WriteFile(p.remoteAddr.String(), fileName); err != nil {
			sendError(conn, p.remoteAddr, err)
			return
		}
	}

	var mode string
	if mode, err = buff.ReadString(0); err != nil {
		conn.WriteTo(errIllegalOperationPacket, p.remoteAddr)
		return
	}
	mode = strings.ToLower(mode[:len(mode)-1])
	if mode != "octet" {
		conn.WriteTo(errIllegalOperationPacket, p.remoteAddr)
		return
	}

	hasOption := false
	if buff.Len() > 0 {
		hasOption = true
	}

	for buff.Len() > 0 {
		var optionName string
		if optionName, err = buff.ReadString(0); err != nil {
			break
		}
		optionName = strings.ToLower(optionName[:len(optionName)-1])
		var value string
		if value, err = buff.ReadString(0); err != nil {
			break
		}
		value = value[:len(value)-1]
		switch optionName {
		case "blksize":
			{
				var size int
				if size, err = strconv.Atoi(value); err != nil {
					break
				}
				// RFC2348 define the minimum size is 8byte
				if size < 8 {
					err = fmt.Errorf("the value of blksize is too small")
					break
				}

				if size < p.blockSize {
					p.blockSize = size
				}
			}
		case "timeout":
			{
				var timeout int
				if timeout, err = strconv.Atoi(value); err != nil {
					break
				}
				// RFC2349 define the minimum timeout is 1second.
				if timeout < 1 {
					err = fmt.Errorf("the value of timeout is invalid")
					break
				}
				if timeout < p.timeout {
					p.timeout = timeout
				}
			}
		case "tsize":
			{
				var tsize int
				if tsize, err = strconv.Atoi(value); err != nil {
					break
				}
				p.transferSize = tsize
			}
		default:
			{
				err = fmt.Errorf("unknown option: %s", optionName)
				break
			}
		}
		if err != nil {
			break
		}
	}

	if err != nil {
		sendError(conn, p.remoteAddr, err)
		return
	}

	if hasOption {
		sendOptionAck(conn, p.remoteAddr, p.blockSize, p.timeout, p.transferSize)
	} else {
		ackpacket := []byte{0x00, 0x04, 0x00, 0x00}
		conn.WriteTo(ackpacket, p.remoteAddr)
	}
	return
}

func (p *clientPeer) HandleWriteData(conn net.PacketConn, buff *bytes.Buffer) {
	if p.writeSeekCloser == nil {
		conn.WriteTo(errIllegalOperationPacket, p.remoteAddr)
		return
	}

	var blockID uint16
	if err := binary.Read(buff, binary.BigEndian, &blockID); err != nil {
		return
	}
	if blockID == 0 {
		conn.WriteTo(errIllegalOperationPacket, p.remoteAddr)
		return
	}
	logln("GET DQ, blockid:", blockID)

	data := buff.Next(buff.Len())
	if _, err := p.writeSeekCloser.Seek(int64(blockID-1)*int64(p.blockSize), 0); err != nil {
		sendError(conn, p.remoteAddr, err)
		return
	}
	if _, err := p.writeSeekCloser.Write(data); err != nil {
		logln("err:", err)
		sendError(conn, p.remoteAddr, err)
		return
	}

	ackpacket := []byte{0x00, 0x04, 0x00, 0x00}
	binary.BigEndian.PutUint16(ackpacket[2:], blockID)
	_, err := conn.WriteTo(ackpacket, p.remoteAddr)
	if err != nil {
		logln("DQ, err:", err)
	}
	logln("ACK blockid:", blockID)
	if len(data) < p.blockSize {
		logln("Final DQ")
	}
}
