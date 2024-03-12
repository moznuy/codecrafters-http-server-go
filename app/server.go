package main

import (
	"fmt"
	"io"
	"net"
	"os"
	"regexp"
	"strings"
)

func root() string {
	return "HTTP/1.1 200 OK\r\n\r\n"
}

var echoRe = regexp.MustCompile(`/echo/(?P<Resource>\S+)`)

func echo(path string) string {
	matches := echoRe.FindStringSubmatch(path)

	if matches == nil {
		panic("did not match")
	}
	resource := matches[1]
	return fmt.Sprintf(
		"HTTP/1.1 200 OK\r\n"+
			"Content-Type: text/plain\r\n"+
			"Content-Length: %v\r\n"+
			"\r\n"+
			"%s", len(resource), resource)
}

func userAgent(request Request) string {
	m, exist := request.Headers["User-Agent"]
	if !exist {
		panic("no user-agent")
	}
	return fmt.Sprintf(
		"HTTP/1.1 200 OK\r\n"+
			"Content-Type: text/plain\r\n"+
			"Content-Length: %v\r\n"+
			"\r\n"+
			"%s", len(m), m)
}

func routes(request Request) string {
	if request.Path == "/" {
		return root()
	}
	if request.Path == "/user-agent" {
		return userAgent(request)
	}
	if echoRe.MatchString(request.Path) {
		return echo(request.Path)
	}
	return "HTTP/1.1 404 Not Found\r\n\r\n"
}

type Request struct {
	Path    string
	Version string
	Headers map[string]string
}

var requestRe = regexp.MustCompile(`GET (?P<Path>\S+) HTTP/(?P<Version>\S+)`)

func ParseRequest(lines []string) Request {
	firstLine := lines[0]
	restLines := lines[1:]
	matches := requestRe.FindStringSubmatch(firstLine)
	if matches == nil {
		panic("Could not parse HTTP first line")
	}

	return Request{
		Path:    matches[1],
		Version: matches[2],
		Headers: ParseHeaders(restLines),
	}
}

var headerRe = regexp.MustCompile(`(?P<Key>\S+): (?P<Value>\S+)`)

func ParseHeaders(restLines []string) map[string]string {
	result := make(map[string]string, len(restLines))
	for _, line := range restLines {
		if line == "" {
			continue
		}
		match := headerRe.FindStringSubmatch(line)
		if match == nil {
			panic("Could not parse HTTP header")
		}
		// TODO: multimap
		result[match[1]] = match[2]
	}
	return result
}

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
	request := ParseRequest(lines)

	resp := routes(request)

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
