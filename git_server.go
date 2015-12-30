// ************************************************************************** //
//                                                                            //
//                                                        :::      ::::::::   //
//   git_server.go                                      :+:      :+:    :+:   //
//                                                    +:+ +:+         +:+     //
//   By: niccheva <niccheva@student.42.fr>          +#+  +:+       +#+        //
//                                                +#+#+#+#+#+   +#+           //
//   Created: 2015/12/07 18:52:47 by niccheva          #+#    #+#             //
//   Updated: 2015/12/07 18:55:07 by niccheva         ###   ########.fr       //
//                                                                            //
// ************************************************************************** //

package main

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/terminal"
	"io"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"syscall"

	"github.com/spacemonkeygo/openssl"
)

var publicKey string
var authServer string

func createServer(config *ssh.ServerConfig) {

	port := os.Getenv("GIT_PORT")
	if port == "" {

		port = "22"

	}

	listener, err := net.Listen("tcp", "0.0.0.0:" + port)
	if err != nil {

		log.Fatal("Fail to listen for connections: ", err)

	}

	log.Println("git server listen to " + port + " port")
	for {

		netConn, err := listener.Accept()
		if err != nil {

			log.Println("Fail to accept incoming connection: ", err)
			continue

		}

		go handleConnection(netConn, config)

	}

}

type exitStatusMsg struct {

	Status uint32

}

func exitStatus(err error) (exitStatusMsg, error) {

	if err != nil {

		if exitErr, ok := err.(*exec.ExitError); ok {

			if status, ok := exitErr.Sys().(syscall.WaitStatus); ok {

				return exitStatusMsg{uint32(status.ExitStatus())}, nil

			}

		}

		return exitStatusMsg{0}, err

	}

	return exitStatusMsg{0}, nil

}

func handleChannel(connection *ssh.ServerConn, newChannel ssh.NewChannel) {

	channel, reqs, err := newChannel.Accept()
	if err != nil {

		log.Println("Could not accept channel: ", err)
		return

	}

	defer channel.Close()
	for req := range reqs {

		switch req.Type {

		case "exec":
			if req.WantReply {

				req.Reply(true, nil)

			}

			log.Println(connection.User() + "@" + connection.RemoteAddr().String() + " want exec " + string(req.Payload[4:]))
			args := strings.Split(string(req.Payload[4:]), " ")
			if args[0] != "git-receive-pack" && args[0] != "git-upload-pack" {

				channel.Stderr().Write([]byte("Sorry, this server just accept git requests.\n"))
				return

			}

			args[1] = strings.Replace(args[1], "'", "", -1)
			pathRepo := strings.Split(args[1], "/")
			name := pathRepo[len(pathRepo)-1:][0]
			jsonStr := []byte(fmt.Sprintf(`{"key":{"key":"%s"}, "project":{"name":"%s"}}`, publicKey, strings.TrimSuffix(name, ".git")))
			url, err := url.Parse("http://" + authServer + ":3000/api/authorization")
			if err != nil {

				log.Println("Fail to parse url: ", err)
				return

			}

			response, err := restClient("POST", url, jsonStr)
			if err != nil {

				log.Println(err)
				return

			}

			if response.StatusCode != 200 {

				channel.Stderr().Write([]byte("(ERROR) Its appear you have no right on this repository.\n"))
				return

			}

			log.Println(connection.User() + "@" + connection.RemoteAddr().String() + " exec " + string(req.Payload[4:]))

			gitDir := os.Getenv("GIT_SERVER_DIRECTORY")
			if gitDir == "" {

				gitDir = "/tmp/"

			}

			cmd := exec.Command("git-shell", "-c", args[0] + " '" + gitDir + name + "'")
			pipeCommand(cmd, channel, channel.Stderr(), channel)
			cmd.Start()
			status, err := exitStatus(cmd.Wait())
			if err != nil {

				log.Println("exitStatus")
				return

			}

			if status.Status != 0 {

				return

			}

			if _, err := channel.SendRequest("exit-status", false, ssh.Marshal(&status)); err != nil {

				log.Println("sendExit")
				return

			}

			return

		case "env":
			if req.WantReply {

				req.Reply(true, nil)

			}

		case "shell":
			channel.Stderr().Write([]byte("Sorry, this server just accept exec requests.\n"))
			return

		}

	}

}

func handleConnection(connection net.Conn, config *ssh.ServerConfig) {

	defer connection.Close()

	log.Println("New connection from ", connection.RemoteAddr().String())

	sshConn, chans, reqs, err := ssh.NewServerConn(connection, config)
	if err != nil {

		log.Println("Fail to handshake: ", err)
		return

	}

	go ssh.DiscardRequests(reqs)
	for ch := range chans {

		if ch.ChannelType() != "session" {

			ch.Reject(ssh.UnknownChannelType, "unknown channel type")
			continue

		}

		go handleChannel(sshConn, ch)

	}

}

func loadPrivateKey(config *ssh.ServerConfig) {

	homeDirectory := os.Getenv("HOME")

	privateBytes, err := ioutil.ReadFile(homeDirectory + "/.ssh/id_rsa")
	if err != nil {

		log.Fatal("Fail to load private key: ", err)

	}

	fmt.Print("Enter passphrase: ")
	passBytes, err := terminal.ReadPassword(0)
	if err != nil {

		log.Fatal("Fail to read password: ", err)

	}
	fmt.Println()

	privateKeySSL, err := openssl.LoadPrivateKeyFromPEMWidthPassword(privateBytes, string(passBytes))
	if err != nil {

		log.Fatal("Fail to parse private key: ", err)

	}

	privateBytes, err = privateKeySSL.MarshalPKCS1PrivateKeyPEM()
	if err != nil {

		log.Fatal("Fail to load private key: ", err)

	}

	privateKey, err := ssh.ParsePrivateKey(privateBytes)
	if err != nil {

		log.Fatal("Fail to parse private key: ", err)

	}

	config.AddHostKey(privateKey)

	log.Println("Private key loaded.")

}

func restClient(method string, url *url.URL, jsonStr []byte) (*http.Response, error) {

	request, err := http.NewRequest(method, url.String(), bytes.NewBuffer(jsonStr))
	if err != nil {

		return nil, fmt.Errorf("Fail to create new request: ", err)

	}

	request.Header.Set("Content-Type", "application/json")
	client := &http.Client{}
	response, err := client.Do(request)
	if err != nil {

		return nil, fmt.Errorf("Fail to do request: ", err)

	}

	return response, nil

}

func publicKeyCallback(connectionMetadata ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {

	publicKey = base64.StdEncoding.EncodeToString(key.Marshal())
	url, err := url.Parse("http://" + authServer + ":3000/api/authentication")
	if err != nil {

		return nil, fmt.Errorf("Fail to parse url: ", err)

	}

	jsonStr := []byte(fmt.Sprintf(`{"key":{"key":"%s"}}`, publicKey))
	response, err := restClient("POST", url, jsonStr)
	if err != nil {

		return nil, err

	}

	defer response.Body.Close()

	if response.StatusCode == 200 {

		log.Println(connectionMetadata.User() + " accepted.")
		return nil, nil

	}

	return nil, fmt.Errorf("Public key rejected for %q.", connectionMetadata.User())

}

func main() {

	logFile := os.Getenv("GIT_SERVER_LOGFILE")
	if logFile == "" {

		logFile = "/tmp/git_server.log"

	}

	f, err := os.OpenFile(logFile, os.O_WRONLY | os.O_CREATE | os.O_APPEND, 0644)
	if err != nil {

		log.Fatal("Can't open log file: ", err)

	}

	defer f.Close()
	log.SetOutput(f)

	config := &ssh.ServerConfig{

		PublicKeyCallback: publicKeyCallback,

	}

	authServer = os.Getenv("AUTH_SERVER")
	if authServer == "" {

		authServer = "127.0.0.1"

	}

	loadPrivateKey(config)
	createServer(config)

}

func pipeCommand(cmd *exec.Cmd, stdout, stderr io.Writer, stdin io.Reader) {

	stdinIn, err := cmd.StdinPipe()
	if err != nil {
		log.Println(err)
		return
	}
	stdoutOut, err := cmd.StdoutPipe()
	if err != nil {
		log.Println(err)
		return
	}
	stderrOut, err := cmd.StderrPipe()
	if err != nil {
		log.Println(err)
		return
	}

	go func() {
		io.Copy(stdinIn, stdin)
		stdinIn.Close()
	}()
	go io.Copy(stdout, stdoutOut)
	go io.Copy(stderr, stderrOut)

}
