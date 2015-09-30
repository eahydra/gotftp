package main

import (
	"fmt"
	"os"
	"time"

	"github.com/eahydra/gotftp"
)

type FileHandler struct{}

func (s *FileHandler) ReadFile(remoteAddr, fileName string) (gotftp.ReadSeekCloser, error) {
	return os.OpenFile(fileName, os.O_RDONLY, os.ModePerm)
}

func (s *FileHandler) WriteFile(remoteAddr, fileName string) (gotftp.WriteSeekCloser, error) {
	return os.OpenFile(fileName, os.O_CREATE|os.O_WRONLY, os.ModePerm)
}

func (s *FileHandler) IsFileExist(remoteAddr, fileName string) (exist bool, err error) {
	f, err := os.OpenFile(fileName, os.O_RDONLY, os.ModePerm)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	f.Close()
	return true, nil
}

func main() {
	if s, err := gotftp.NewServer(":69", &FileHandler{}, time.Duration(3)*time.Second); err == nil {
		defer s.Close()
		if err = s.Run(); err != nil {
			fmt.Println("err:", err)
		}
	} else {
		fmt.Println("err:", err)
	}
}
