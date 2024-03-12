package main

import (
	"fmt"
	"io"
	"net"
	"os"
)

func main() {
	fmt.Println("Listening on 0.0.0.0:4221")
	l, err := net.Listen("tcp", "0.0.0.0:4221")
	if err != nil {
		fmt.Println("Failed to bind to port 4221")
		os.Exit(1)
	}

	client, err := l.Accept()
	if err != nil {
		fmt.Println("Error accepting connection: ", err.Error())
		os.Exit(1)
	}

	var data [4096]byte
	//for {
	read, err := client.Read(data[:])
	s := string(data[:read])
	//fmt.Printf("read: %v %#v\n", read, data[:read])
	fmt.Printf("%s", s)
	if err != nil {
		if err == io.EOF {
			os.Exit(1) // break
		}
		fmt.Println("Error reading connection: ", err.Error())
		os.Exit(1)
	}

	_, err = client.Write([]byte("HTTP/1.1 200 OK\r\n\r\n"))
	if err != nil {
		fmt.Println("Error writing connection: ", err.Error())
		os.Exit(1)
	}

	//}
	fmt.Println("client disconnected")
}
