package gotftp

import (
	"bytes"
	"encoding/binary"
	"io"
	"net"
	"time"
)

type Client struct {
	remoteAddr net.Addr
	conn       net.PacketConn
	timeout    time.Duration
	retryTime  int
}

func NewClient(addr string, timeout time.Duration, retryTime int) (*Client, error) {
	var raddr net.Addr
	raddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return nil, err
	}
	conn, err := net.ListenPacket("udp4", ":0")
	if err != nil {
		return nil, err
	}
	return &Client{
		conn:       conn,
		remoteAddr: raddr,
		timeout:    timeout,
		retryTime:  retryTime,
	}, nil
}

func (c *Client) Close() error {
	return c.conn.Close()
}

func (c *Client) Get(fileName string, writer io.WriterAt) error {
	rrq := bytes.NewBuffer(nil)
	binary.Write(rrq, binary.BigEndian, uint16(0x01))
	rrq.WriteString(fileName)
	rrq.WriteByte(0)
	rrq.WriteString("octet")
	rrq.WriteByte(0)
	if _, err := c.conn.WriteTo(rrq.Bytes(), c.remoteAddr); err != nil {
		return err
	}

	data := make([]byte, 1024)
	retryTime := 0
readLoop:
	for {
		c.conn.SetReadDeadline(time.Now().Add(c.timeout))
		n, remoteAddr, err := c.conn.ReadFrom(data)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() && retryTime < c.retryTime {
				retryTime++
				continue
			}
			return err
		}
		retryTime = 0
		buff := bytes.NewBuffer(data[:n])
		var operation uint16
		if err = binary.Read(buff, binary.BigEndian, &operation); err != nil {
			continue
		}
		switch operation {
		case 3: // data packet
			{
				var blockID uint16
				if err := binary.Read(buff, binary.BigEndian, &blockID); err != nil {
					continue readLoop
				}
				content := buff.Next(buff.Len())
				if _, err := writer.WriteAt(content, int64(blockID-1)*512); err == nil {
					ackpacket := []byte{0x00, 0x04, 0x00, 0x00}
					binary.BigEndian.PutUint16(ackpacket[2:], blockID)
					c.conn.WriteTo(ackpacket, remoteAddr)
				}
				if len(content) < 512 {
					break readLoop
				}
			}
		case 5: // error packet
			{
				return handleError(buff)
			}
		}
	}
	return nil
}

func (c *Client) Put(fileName string, reader io.ReaderAt) error {
	wrq := bytes.NewBuffer(nil)
	binary.Write(wrq, binary.BigEndian, uint16(0x02))
	wrq.WriteString(fileName)
	wrq.WriteByte(0)
	wrq.WriteString("octet")
	wrq.WriteByte(0)
	if _, err := c.conn.WriteTo(wrq.Bytes(), c.remoteAddr); err != nil {
		return err
	}
	data := make([]byte, 1024)
	retryTime := 0
writeLoop:
	for {
		c.conn.SetReadDeadline(time.Now().Add(c.timeout))
		n, remoteAddr, err := c.conn.ReadFrom(data)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() && retryTime < c.retryTime {
				retryTime++
				continue
			}
			return err
		}
		retryTime = 0
		buff := bytes.NewBuffer(data[:n])
		var operation uint16
		if err = binary.Read(buff, binary.BigEndian, &operation); err != nil {
			continue
		}
		switch operation {
		case 4: // ack packet
			{
				var blockID uint16
				if err := binary.Read(buff, binary.BigEndian, &blockID); err != nil {
					continue writeLoop
				}
				binary.BigEndian.PutUint16(data[0:2], uint16(0x03))
				binary.BigEndian.PutUint16(data[2:], blockID+1)
				n, err := reader.ReadAt(data[4:516], int64(blockID)*512)
				if err != nil {
					if err == io.EOF {
						err = nil
						break writeLoop
					}
					sendError(c.conn, remoteAddr, err)
					return err
				}
				if _, err := c.conn.WriteTo(data[:n+4], remoteAddr); err != nil {
					return err
				}
			}
		case 5: // error packet
			{
				return handleError(buff)
			}
		}
	}
	return nil
}
