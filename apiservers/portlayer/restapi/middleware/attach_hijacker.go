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

package middleware

import (
	"bufio"
	"encoding/json"
	"fmt"
	//	"io"
	"net/http"
	"net/url"
	//	goexec "os/exec"
	"strings"
	"time"

	"golang.org/x/net/context"

	log "github.com/Sirupsen/logrus"

	"github.com/vmware/vic/apiservers/portlayer/models"
	"github.com/vmware/vic/pkg/attachutils"
	"github.com/vmware/vic/portlayer/attach"
)

type AttachHijacker struct {
	origHandler  http.Handler
	attachServer *attach.Server

	containerID string
	stdin       bool
	stdout      bool
	stderr      bool
	stream      bool
}

const basePath = "/containers"
const operation = "/attach"

//NewAttachHijacker handles attach for the interaction handler.  With the current
//version of go-swagger, we cannot implement attach with a standard swagger
//handler.
func NewAttachHijacker(orig http.Handler, as *attach.Server) *AttachHijacker {
	return &AttachHijacker{origHandler: orig, attachServer: as}
}

func (a *AttachHijacker) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	// Make sure the call is for /containers/{id}/attach
	if !strings.HasPrefix(r.URL.Path, basePath) || !strings.Contains(r.URL.Path, operation) {
		a.origHandler.ServeHTTP(rw, r)
		return
	}

	code, err := a.GetParams(r)
	if err != nil {
		log.Errorf("interaction attach container failed %#v\n", err)
		a.WritePortlayerError(rw, code, err.Error())
		return
	}

	getStream := attachutils.HijackConnection(rw, r)
	if getStream == nil {
		a.WritePortlayerError(rw, http.StatusInternalServerError, "HTTP Hijack failed")
		return
	}

	// Get the stdio streams from the hijacked connection
	hijackedStdin, hijackedStdout, hijackedStderr, err := getStream()
	if err != nil {
		a.WritePortlayerError(rw, http.StatusInternalServerError, "Error getting hijacked streams")
		return
	}

	log.Printf("Attempting to get ssh session for container %s", a.containerID)
	// Get the ssh session streams
	connContainer, err := a.attachServer.Get(context.Background(), a.containerID, 600*time.Second)

	// FIXME - We need to retrieve the TTY status from the container's guestinfo.  This
	// information should have been passed to the Exec port layer during 'docker create'.
	// Set to true till we have that code

	// Now cross the streams of the hijacked connection to the container's stdio streams
	outContainer := bufio.NewReader(connContainer.Stdout())
	err = attachutils.CopyWaitStreamsToStreams(true, hijackedStdin, connContainer.Stdin(), hijackedStdout, outContainer, hijackedStderr)

	//	TestAttachEnclosed(streamConfig, attachConfig)

	//We return errors, but it really doesn't matter.  We've hijacked the connection
	//from both go-swagger and the go http handlers.
	if err != nil {
		a.WritePortlayerError(rw, http.StatusBadRequest, "Error attaching the connection to the container stio streams")
		return
	}

	//Return success
	a.WritePortlayerSuccess(rw)
}

//func TestAttachEnclosed(sc *runconfig.StreamConfig, ca *backend.ContainerAttachConfig) {
//	cmd := goexec.Command("/home/loc/go/src/github.com/test/test")

//	inStream, outStream, _, err := ca.GetStreams()
//	cmd.Stdout = sc.Stdout()
//	cmd.Stdin = sc.Stdin()

//	err = cmd.Start()
//	if err != nil {
//		log.Panic(err)
//	}

//	go func() {
//		_, err := io.Copy(sc.StdinPipe(), inStream)

//		if err != nil {
//			fmt.Printf("Got an error copying stdin - %+v\n", err)
//		}
//	}()
//	go func() {
//		io.Copy(outStream, sc.StdoutPipe())
//		fmt.Printf("Finished copying stdout\n")
//	}()
//}

func (a *AttachHijacker) GetParams(r *http.Request) (int, error) {
	//Extract container id from path.
	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 4 {
		return http.StatusBadRequest, fmt.Errorf("Invalid URL for container attach")
	}

	a.containerID = parts[2]

	//Extract boolean values stdin, stdout, stderr, and stream from query inputs
	values, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return http.StatusBadRequest, fmt.Errorf("Could not retrieve request input for container attach")
	}

	if parts, exist := values["stdin"]; exist {
		if len(parts) > 0 {
			a.stdin = (strings.Compare("1", parts[0]) == 0)
		}
	} else {
		a.stdin = false
	}

	if parts, exist := values["stdout"]; exist {
		if len(parts) > 0 {
			a.stdout = (strings.Compare("1", parts[0]) == 0)
		}
	} else {
		a.stdout = false
	}

	if parts, exist := values["stderr"]; exist {
		if len(parts) > 0 {
			a.stderr = (strings.Compare("1", parts[0]) == 0)
		}
	} else {
		a.stderr = false
	}

	if parts, exist := values["stream"]; exist {
		if len(parts) > 0 {
			a.stream = (strings.Compare("1", parts[0]) == 0)
		}
	} else {
		a.stream = false
	}

	return http.StatusOK, nil
}

func (a *AttachHijacker) WritePortlayerError(rw http.ResponseWriter, statusCode int, errMsg string) {
	retErr := &models.Error{Message: errMsg}

	rw.Header().Set("Content-Type", "application/json")
	rw.WriteHeader(statusCode)

	json.NewEncoder(rw).Encode(retErr)
}

func (a *AttachHijacker) WritePortlayerSuccess(rw http.ResponseWriter) {
	rw.WriteHeader(http.StatusOK)
	rw.Write([]byte("OK"))
}
