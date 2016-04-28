// Copyright 2016 VMware, Inc. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package attachutils

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"time"

	log "github.com/Sirupsen/logrus"

	"github.com/docker/docker/api/server/httputils"
	"github.com/docker/docker/pkg/ioutils"
	"github.com/docker/docker/pkg/stdcopy"
)

// HijackClosure function type returned from HijackConnection().  The call can
// later be used to return stdio streams for the connection.
type HijackClosure func() (io.ReadCloser, io.Writer, io.Writer, error)

// AttachToDockerLikeServer is a utility function that assumes the server that
// is being attempted for attach follows a URL path along the line of
// /containers/{containerID}/attach?
func AttachToDockerLikeServer(baseURL, containerID, attachVerb string, useStdin, useStdout, useStderr, stream bool) (net.Conn, *bufio.Reader, error) {
	var options map[string]string
	var buf bytes.Buffer

	// Build up the request endpoint with params from ContainerAttachConfig
	options = make(map[string]string)

	if useStdin {
		options["stdin"] = "1"
	}

	if useStdout {
		options["stdout"] = "1"
	}

	if useStderr {
		options["stderr"] = "1"
	}

	if stream {
		options["stream"] = "1"
	}

	for key, value := range options {
		if buf.Len() > 0 {
			buf.WriteString("&")
		}
		buf.WriteString(key)
		buf.WriteString("=")
		buf.WriteString(value)
	}

	endpoint := baseURL + "/containers/" + containerID + "/attach?" + buf.String()

	return AttachToServer(endpoint)
}

// AttachToServer connects to a server, given the address in 'server' and returns
// a net conn and buffered reader.
func AttachToServer(server string) (net.Conn, *bufio.Reader, error) {
	method := "POST"

	// Create a connection client and request
	c, err := net.DialTimeout("tcp", server, time.Duration(10*time.Second))
	if err != nil {
		retErr := fmt.Errorf("could not dial VIC portlayer for attach: %v", err)
		log.Errorf(retErr.Error())
		return nil, nil, retErr
	}

	if tcpConn, ok := c.(*net.TCPConn); ok {
		tcpConn.SetKeepAlive(true)
		tcpConn.SetKeepAlivePeriod(30 * time.Second)
	}

	client := httputil.NewClientConn(c, nil)
	defer client.Close()

	req, err := http.NewRequest(method, "http://"+server, nil)
	if err != nil {
		retErr := fmt.Errorf("could not create request for VIC portlayer attach: %v", err)
		log.Errorf(retErr.Error())
		return nil, nil, retErr
	}

	//	req.Header.Set("Connection", "Upgrade")
	//	req.Header.Set("Upgrade", "tcp")
	req.Header.Set("Content-Type", "text/plain")

	// Make the connection and hijack it
	client.Do(req)
	conn, br := client.Hijack()

	return conn, br, nil
}

// CloseWriter is an interface that implement structs
// that close input streams to prevent from writing.
type CloseWriter interface {
	CloseWrite() error
}

// CopyWaitConnToStreams copies data between a set of stdio streams and a conn
// and buffered reader objtained from a hijacked connection to a server.
func CopyWaitConnToStreams(useTty bool, inStream io.ReadCloser, outStream, errStream io.Writer, conn net.Conn, bufReader *bufio.Reader) error {
	var err error

	// Setup go func to copy from api server input conn to portlayer
	receiveStdout := make(chan error, 1)
	if outStream != nil || errStream != nil {
		go func() {
			// When TTY is ON, use regular copy
			//			if ca.
			if useTty && outStream != nil {
				log.Printf("\tTTY on, stdout - regular copy from server...")
				_, err = io.Copy(outStream, bufReader)
			} else {
				log.Printf("\tTTY off, special copy from server...")
				_, err = stdcopy.StdCopy(outStream, errStream, bufReader)
			}
			log.Debugf("End of stdout")
			receiveStdout <- err
		}()
	}

	// Setup go func to copy from portlayer to api server conn
	stdinDone := make(chan struct{})
	go func() {
		log.Printf("Checking if we need to handle stdin...")
		if inStream != nil {
			log.Printf("\tstdin - Copying to the container...")
			_, err = io.Copy(conn, inStream)
			if err != nil {
				log.Errorf("Couldn't copy streams: %s", err)
			}
			log.Debugf("End of stdin")
		}

		log.Printf("Attempt to close connection to server")
		// if conn is a type that has CloseWrite, we call it.
		if writeCloser, isa := conn.(CloseWriter); isa {
			if err := writeCloser.CloseWrite(); err != nil {
				log.Errorf("Couldn't send EOF: %s", err)
			}
		}
		log.Printf("Closing stdin")
		close(stdinDone)
	}()

	select {
	case err := <-receiveStdout:
		if err != nil {
			log.Errorf("Error receiveStdout: %s", err)
			return err
		}
	case <-stdinDone:
		if outStream != nil || errStream != nil {
			if err := <-receiveStdout; err != nil {
				log.Errorf("Error receiveStdout: %s", err)
				return err
			}
		}
	}

	return nil
}

// CopyWaitStreamsToStreams copies stdin from client to server and stdout,stderr from server to client
func CopyWaitStreamsToStreams(useTty bool, inClient io.ReadCloser, inServer io.Writer, outClient io.Writer, outServer *bufio.Reader, errClient io.Writer) error {
	var err error

	// Setup go func to copy from api server input conn to portlayer
	receiveStdout := make(chan error, 1)
	if outClient != nil || errClient != nil {
		go func() {
			// When TTY is ON, use regular copy
			//			if ca.
			if useTty && outClient != nil {
				log.Printf("\tTTY on, stdout - regular copy from server...")
				_, err = io.Copy(outClient, outServer)
			} else {
				log.Printf("\tTTY off, special copy from server...")
				_, err = stdcopy.StdCopy(outClient, errClient, outServer)
			}
			log.Debugf("End of stdout")
			receiveStdout <- err
		}()
	}

	// Setup go func to copy from portlayer to api server conn
	stdinDone := make(chan struct{})
	go func() {
		log.Printf("Checking if we need to handle stdin...")
		if inClient != nil {
			log.Printf("\tstdin - Copying to the container...")
			_, err = io.Copy(inServer, inClient)
			if err != nil {
				log.Errorf("Couldn't copy streams: %s", err)
			}
			log.Debugf("End of stdin")
		}

		// FIXME - How do we close the server connection in this case?

		log.Printf("Closing stdin")
		close(stdinDone)
	}()

	select {
	case err := <-receiveStdout:
		if err != nil {
			log.Errorf("Error receiveStdout: %s", err)
			return err
		}
	case <-stdinDone:
		if outClient != nil || errClient != nil {
			if err := <-receiveStdout; err != nil {
				log.Errorf("Error receiveStdout: %s", err)
				return err
			}
		}
	}

	return nil
}

// HijackConnection is a helper function to call from HTTP middleware that has
// access to the http response writer.
func HijackConnection(rw http.ResponseWriter, r *http.Request) HijackClosure {
	_, upgrade := r.Header["Upgrade"]

	hijacker, ok := rw.(http.Hijacker)
	if !ok {
		return nil
	}

	conn, _, err := hijacker.Hijack()
	if err != nil {
		return nil
	}

	// GetStreams hijacks the the connection from the http handler. This code originated
	// in the docker engine-api image router.
	setupStreams := func() (io.ReadCloser, io.Writer, io.Writer, error) {
		// set raw mode
		conn.Write([]byte{})

		if upgrade {
			fmt.Fprintf(conn, "HTTP/1.1 101 UPGRADED\r\nContent-Type: application/vnd.vmware.raw-stream\r\nConnection: Upgrade\r\nUpgrade: tcp\r\n\r\n")
		} else {
			fmt.Fprintf(conn, "HTTP/1.1 200 OK\r\nContent-Type: application/vnd.vmware.raw-stream\r\n\r\n")
		}

		closer := func() error {
			httputils.CloseStreams(conn)
			return nil
		}
		return ioutils.NewReadCloserWrapper(conn, closer), conn, conn, nil
	}

	return setupStreams
}
