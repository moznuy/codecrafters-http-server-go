package main

import (
	"fmt"
	"io"
	"net"
	"os"
	"regexp"
	"strings"
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
	req := ""
	for {
		read, err := client.Read(data[:])
		s := string(data[:read])
		//fmt.Printf("%s", s)
		req += s
		if strings.HasSuffix(req, "\r\n\r\n") {
			fmt.Println("Request finished")
			break
		}

		if err != nil {
			if err == io.EOF {
				fmt.Println("Connection closed")
				os.Exit(1) // break
			}
			fmt.Println("Error reading connection: ", err.Error())
			os.Exit(1)
		}
	}

	lines := strings.Split(req, "\r\n")
	//for _, line := range lines {
	//	fmt.Printf("%s\n", line)
	//}
	r := regexp.MustCompile(`GET (?P<Path>\S+) HTTP/(?P<Version>\S+)`)
	matches := r.FindStringSubmatch(lines[0])
	if matches == nil {
		fmt.Println("Could not parse HTTP header")
		os.Exit(1)
	}
	path := matches[1]
	resp := ""
	if path == "/" {
		resp = "HTTP/1.1 200 OK\r\n\r\n"
	} else {
		r := regexp.MustCompile(`/echo/(?P<Resource>\S+)`)
		matches := r.FindStringSubmatch(path)
		if matches == nil {
			resp = "HTTP/1.1 404 Not Found\r\n\r\n"
		} else {
			resource := matches[1]
			resp = fmt.Sprintf(
				"HTTP/1.1 200 OK\r\n"+
					"Content-Type: text/plain\r\n"+
					"Content-Length: %v\r\n"+
					"\r\n"+
					"%s", len(resource), resource)
		}

	}

	_, err = client.Write([]byte(resp))
	if err != nil {
		fmt.Println("Error writing connection: ", err.Error())
		os.Exit(1)
	}
	err = client.Close()
	if err != nil {
		fmt.Println("Error writing connection: ", err.Error())
		os.Exit(1)
	}

	fmt.Println("client disconnected")
}
