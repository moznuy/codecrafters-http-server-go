package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

func root(request Request) {
	//TODO: handle err
	request.writer.Write([]byte("HTTP/1.1 200 OK\r\n\r\n"))
}

var echoRe = regexp.MustCompile(`/echo/(?P<Resource>\S+)`)
var filesRe = regexp.MustCompile(`/files/(?P<Filename>\S+)`)

func echo(request Request) {
	matches := echoRe.FindStringSubmatch(request.Path)

	if matches == nil {
		panic("did not match")
	}
	resource := matches[1]

	response := fmt.Sprintf(
		"HTTP/1.1 200 OK\r\n"+
			"Content-Type: text/plain\r\n"+
			"Content-Length: %v\r\n"+
			"\r\n"+
			"%s", len(resource), resource)
	//TODO: handle err
	request.writer.Write([]byte(response))
}

func files(request Request) {
	matches := filesRe.FindStringSubmatch(request.Path)

	if matches == nil {
		panic("did not match")
	}
	filename := matches[1]

	if strings.ToUpper(request.Method) == "POST" {
		file, err := os.OpenFile(filename, os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			panic("open failed")
		}
		defer file.Close()
		reader := bytes.NewReader(request.Body)
		//TODO: handle err
		io.Copy(file, reader)
		request.writer.Write([]byte("HTTP/1.1 201 Created\r\n\r\n"))
		return
	}

	info, err := os.Stat(filename)
	if errors.Is(err, os.ErrNotExist) {
		//TODO: handle err
		request.writer.Write([]byte("HTTP/1.1 404 Not Found\r\n\r\n"))
		return
	}

	file, err := os.Open(filename)
	if err != nil {
		panic("open failed")
	}
	//TODO: handle err
	defer file.Close()
	//TODO: handle err
	response := fmt.Sprintf("HTTP/1.1 200 OK\r\nContent-Type: application/octet-stream\r\nContent-Length: %v\r\n\r\n", info.Size())
	request.writer.Write([]byte(response))
	//TODO: handle err
	io.Copy(request.writer, file)
}

func userAgent(request Request) {
	m, exist := request.Headers["User-Agent"]
	if !exist {
		panic("no user-agent")
	}
	response := fmt.Sprintf(
		"HTTP/1.1 200 OK\r\n"+
			"Content-Type: text/plain\r\n"+
			"Content-Length: %v\r\n"+
			"\r\n"+
			"%s", len(m), m)
	//TODO: handle err
	request.writer.Write([]byte(response))
}

func routes(request Request) {
	if request.Path == "/" {
		root(request)
		return
	}
	if request.Path == "/user-agent" {
		userAgent(request)
		return
	}
	if echoRe.MatchString(request.Path) {
		echo(request)
		return
	}
	if filesRe.MatchString(request.Path) {
		files(request)
		return
	}
	//TODO: handle err
	request.writer.Write([]byte("HTTP/1.1 404 Not Found\r\n\r\n"))
}

type Request struct {
	Method  string
	Path    string
	Version string
	Headers map[string]string
	writer  io.Writer
	Body    []byte
}

var requestRe = regexp.MustCompile(`(?P<Method>\S+) (?P<Path>\S+) HTTP/(?P<Version>\S+)`)

func ParseRequest(lines []string, client net.Conn) Request {
	firstLine := lines[0]
	restLines := lines[1:]
	matches := requestRe.FindStringSubmatch(firstLine)
	if matches == nil {
		panic("Could not parse HTTP first line")
	}

	return Request{
		Method:  matches[1],
		Path:    matches[2],
		Version: matches[3],
		Headers: ParseHeaders(restLines),
		writer:  client,
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

func respond(request Request, client net.Conn) {
	routes(request)

	//_, err := client.Write([]byte(resp))
	//if err != nil {
	//	fmt.Println("Error writing connection: ", err.Error())
	//	return
	//}
	err := client.Close()
	if err != nil {
		fmt.Println("Error closing connection: ", err.Error())
		return
	}
}

func readMessages(messages <-chan ClientMessage) {
	requests := make(map[int][]byte)
	for message := range messages {
		req := requests[message.ClientId]
		requests[message.ClientId] = append(req, message.Data...)
		cur_data := requests[message.ClientId]

		slices := bytes.SplitAfterN(cur_data, []byte("\r\n\r\n"), 2)
		if len(slices) < 2 {
			continue
		}
		if len(slices) > 2 {
			panic("wrong number of slices")
		}

		// TODO: only once:
		header := string(slices[0])
		lines := strings.Split(header, "\r\n")
		request := ParseRequest(lines, message.Client)
		lengthRaw, exist := request.Headers["Content-Length"]
		if !exist {
			lengthRaw = "0"
		}
		length, err := strconv.Atoi(lengthRaw)
		if err != nil {
			panic("Could not parse content-length")
		}
		if len(slices[1]) < length {
			continue
		}
		// TODO: Reader instead of all body
		request.Body = slices[1]

		delete(requests, message.ClientId)
		fmt.Println("Request parsing finished")
		respond(request, message.Client)
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
	dir := flag.String("directory", ".", "")
	flag.Parse()
	fmt.Printf("Working directory %q\n", *dir)

	err := os.Chdir(*dir)
	//entries, err := os.ReadDir("./")
	//for _, e := range entries {
	//	fmt.Println(e.Name())
	//}

	if err != nil {
		fmt.Println("Error changing directory: ", err.Error())
		os.Exit(1)
	}

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
