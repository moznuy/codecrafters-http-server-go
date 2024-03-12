package main

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
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

type ClientMessage struct {
	ClientId int
	Client   net.Conn
	Data     []byte
}

func clientRead(clientId int, client net.Conn, done *atomic.Bool, wg *sync.WaitGroup, messages chan<- ClientMessage) {
	//result := make(chan ClientMessage)
	wg.Add(1)
	go func() {
		defer wg.Done()
		var data [4096]byte
		for {
			if done.Load() {
				fmt.Println("Done fired")
				break
			}
			err := client.SetReadDeadline(time.Now().Add(10 * time.Second))
			if err != nil {
				fmt.Println("Error SetReadDeadline", err.Error())
				os.Exit(1)
			}
			read, err := client.Read(data[:])
			if read > 0 {
				messages <- ClientMessage{
					ClientId: clientId,
					Client:   client,
					Data:     data[:read],
				}
			}

			if err != nil {
				if errors.Is(err, io.EOF) {
					fmt.Println("Connection closed")
					return
				}
				if errors.Is(err, os.ErrDeadlineExceeded) {
					fmt.Println("ErrDeadlineExceeded")
					continue
				}
				if errors.Is(err, net.ErrClosed) {
					fmt.Println("read: Connection closed")
					break
				}
				fmt.Println("Error reading connection: ", err.Error())
				os.Exit(1)
			}
		}
	}()
}

func respond(req string, client net.Conn) {
	lines := strings.Split(req, "\r\n")
	request := ParseRequest(lines)
	resp := routes(request)

	_, err := client.Write([]byte(resp))
	if err != nil {
		fmt.Println("Error writing connection: ", err.Error())
		return
	}
	err = client.Close()
	if err != nil {
		fmt.Println("Error closing connection: ", err.Error())
		return
	}
}

func readMessages(messages <-chan ClientMessage) {
	requests := make(map[int]string)
	for message := range messages {
		req := requests[message.ClientId]
		req += string(message.Data)
		requests[message.ClientId] = req

		if !strings.HasSuffix(req, "\r\n\r\n") {
			continue
		}
		delete(requests, message.ClientId)
		fmt.Println("Request parsing finished")
		respond(req, message.Client)
	}
}

func sigHandler(sigs <-chan os.Signal, listener net.Listener) {
	<-sigs
	fmt.Println("Close signal")
	err := listener.Close()
	if err != nil {
		fmt.Println("Error closing listener: ", err.Error())
		os.Exit(1)
	}
}

func main() {
	sigs := make(chan os.Signal, 1)

	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	fmt.Println("Listening on 0.0.0.0:4221")
	listener, err := net.Listen("tcp", "0.0.0.0:4221")
	if err != nil {
		fmt.Println("Failed to bind to port 4221")
		os.Exit(1)
	}
	go sigHandler(sigs, listener)

	done := atomic.Bool{}
	done.Store(false)
	wg := sync.WaitGroup{}
	clientMessages := make(chan ClientMessage)
	go readMessages(clientMessages)

	for clientID := 0; ; clientID += 1 {
		client, err := listener.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				fmt.Println("listen: Connection closed")
				break
			}
			fmt.Println("Error accepting connection: ", err.Error())
			break
		}

		clientRead(clientID, client, &done, &wg, clientMessages)
	}
	done.Store(true)
	wg.Wait()
}
