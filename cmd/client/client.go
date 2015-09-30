package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/eahydra/gotftp"
)

func main() {
	var (
		get      bool
		put      bool
		srcFile  string
		destFile string
		addr     string
	)

	flag.StringVar(&addr, "addr", "", "addr=x.x.x.x:69")
	flag.StringVar(&srcFile, "src", "", "src=xxxx.file")
	flag.StringVar(&destFile, "dst", "", "dst=xxxx.file")
	flag.BoolVar(&get, "get", false, "get src=xxxx.file")
	flag.BoolVar(&put, "put", false, "put src=xxxx.file dst=yyyy.file")
	flag.Parse()

	if len(addr) == 0 {
		fmt.Println("invalid command, please set remote address")
		return
	}
	client, err := gotftp.NewClient(addr, time.Duration(3)*time.Second, 3)
	if err != nil {
		fmt.Println("err:", err)
		return
	}
	defer client.Close()

	if get {
		if len(srcFile) == 0 {
			fmt.Println("invalid command, please set source file name")
			return
		}
		f, err := os.OpenFile("./"+srcFile, os.O_CREATE|os.O_WRONLY, os.ModePerm)
		if err != nil {
			fmt.Println("err:", err.Error())
			return
		}
		defer f.Close()
		err = client.Get(srcFile, f)
		if err != nil {
			fmt.Println("err:", err.Error())
		}
	} else if put {
		if len(srcFile) == 0 || len(destFile) == 0 {
			fmt.Println("invalid command")
			return
		}
		f, err := os.OpenFile(srcFile, os.O_RDONLY, os.ModePerm)
		if err != nil {
			fmt.Println("err:", err.Error())
			return
		}
		defer f.Close()
		err = client.Put(destFile, f)
		if err != nil {
			fmt.Println("err:", err.Error())
		}
	}
}
