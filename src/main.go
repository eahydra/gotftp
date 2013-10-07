package main

import (
	"flag"
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

func runServer() {
	fmt.Println("Begin Run")
	defer fmt.Println("End Run")
	s := gotftp.NewServer(&ServerHandler{}, time.Duration(2)*time.Second, time.Duration(3)*time.Second)
	err := s.Run(":69")
	if err != nil {
		fmt.Println("err:", err.Error())
	}
}

func main() {
	var server bool
	var get bool
	var put bool
	var srcFile string
	var destFile string
	var addr string
	flag.StringVar(&addr, "addr", "", "addr=x.x.x.x:69")
	flag.StringVar(&srcFile, "src", "", "src=xxxx.file")
	flag.StringVar(&destFile, "dst", "", "dst=xxxx.file")
	flag.BoolVar(&get, "get", false, "get src=xxxx.file")
	flag.BoolVar(&put, "put", false, "put src=xxxx.file dst=yyyy.file")
	flag.BoolVar(&server, "svr", false, "svr")
	flag.Parse()
	if get {
		if srcFile == "" || addr == "" {
			println("invalid command")
			return
		}
		f, err := os.OpenFile("./"+srcFile, os.O_CREATE|os.O_WRONLY, os.ModePerm)
		if err != nil {
			println("err:", err.Error())
			return
		}
		defer f.Close()
		err = gotftp.ReadFile(addr, srcFile, f)
		if err != nil {
			println("err:", err.Error())
		}
	} else if put {
		if srcFile == "" || destFile == "" || addr == "" {
			println("invalid command")
			return
		}
		f, err := os.OpenFile(srcFile, os.O_RDONLY, os.ModePerm)
		if err != nil {
			println("err:", err.Error())
			return
		}
		defer f.Close()
		err = gotftp.WriteFile(addr, destFile, f)
		if err != nil {
			println("err:", err.Error())
		}
	} else if server {
		runServer()
	} else {
		println("invalid command")
	}
}
