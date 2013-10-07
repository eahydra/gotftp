package main

import (
	"fmt"
	"gotftp"
	"os"
	"time"
)

// ServerHandler - used for handle TFTP request
type ServerHandler struct{}

type File struct {
	*os.File
}

func (f *File) Size() (n int64, err error) {
	info, err := f.Stat()
	if err != nil {
		return 0, err
	}
	return info.Size(), nil
}

// ReadFile - when one TFTP RRQ come in, it will be called.
func (s *ServerHandler) ReadFile(file string) (rc gotftp.ReadCloser, err error) {
	f, err := os.OpenFile("./"+file, os.O_RDONLY, os.ModePerm)
	if err != nil {
		return nil, err
	}
	return &File{File: f}, nil
}

// WriteFile - when TFTP WWQ come in, it will be called
func (s *ServerHandler) WriteFile(file string) (wc gotftp.WriteCloser, err error) {
	f, err := os.OpenFile("./"+file, os.O_WRONLY|os.O_CREATE, os.ModePerm)
	if err != nil {
		return nil, err
	}
	return &File{File: f}, nil
}

func main() {
	fmt.Println("Begin Run")
	defer fmt.Println("End Run")
	s := gotftp.NewServer(&ServerHandler{}, time.Duration(2)*time.Second, time.Duration(3)*time.Second)
	err := s.Run(":69")
	if err != nil {
		fmt.Println("err:", err.Error())
	}
}
