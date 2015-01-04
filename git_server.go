// ************************************************************************** //
//                                                                            //
//                                                        :::      ::::::::   //
//   git_server.go                                      :+:      :+:    :+:   //
//                                                    +:+ +:+         +:+     //
//   By: niccheva <niccheva@student.42.fr>          +#+  +:+       +#+        //
//                                                +#+#+#+#+#+   +#+           //
//   Created: 2015/01/04 18:39:27 by niccheva          #+#    #+#             //
//   Updated: 2015/01/04 22:07:51 by niccheva         ###   ########.fr       //
//                                                                            //
// ************************************************************************** //

package main

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"os"
	"strings"
	"io"
	"os/exec"
	"syscall"

	"code.google.com/p/go.crypto/ssh"
)

func parseKeys(conf *ssh.ServerConfig) {
	privateBytes, err := ioutil.ReadFile(os.Getenv("HOME") + "/.ssh/id_rsa")
	if err != nil {
		log.Fatal("Failed to load private key: ", err)
	}

	private, err := ssh.ParsePrivateKey(privateBytes)
	if err != nil {
		log.Fatal("Failed to parse private key: ", err, "\n\n Maybe your key is encrypted, please try with an not encrypted key..")
	}

	conf.AddHostKey(private)
}

func createServer(conf *ssh.ServerConfig) {
	listener, err := net.Listen("tcp", "0.0.0.0:22")
	if err != nil {
		log.Fatal("Failed to listen for connection: ", err)
	}

	for {
		nConn, err := listener.Accept()
		if err != nil {
			log.Println("Failed to accept incoming connection: ", err)
			continue
		}
		go handleConnection(nConn, conf)
	}
}

func handleConnection(conn net.Conn, conf *ssh.ServerConfig) {
	defer conn.Close()

	sshConn, chans, reqs, err := ssh.NewServerConn(conn, conf)
	if err != nil {
		log.Fatal("Failed to handshake: ", err)
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

func writeLog(buf bytes.Buffer) {
	log.Print(&buf)
	logfile := os.Getenv("GIT_LOGFILE")
	if logfile == "" {
		logfile = os.Getenv("HOME") + "/git/.log"
	}

	f, err := os.OpenFile(logfile, os.O_APPEND | os.O_WRONLY, 0644)
	if err != nil {
		log.Println(err)
		return
	}

	defer f.Close()

	_, err = f.WriteString(buf.String())
	if err != nil {
		log.Println(err)
		return
	}
}

func handleChannel(conn *ssh.ServerConn, newChannel ssh.NewChannel) {
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
			var buf bytes.Buffer
			logger := log.New(&buf, "", log.LstdFlags)
			logger.Println(string(req.Payload[4:]))
			writeLog(buf)

			args := strings.Split(string(req.Payload[4:]), " ")
			if args[0] == "init" {
				return
			}
			if args[0] != "git-receive-pack" && args[0] != "git-upload-pack" {
				channel.Stderr().Write([]byte("Sorry, this server just accept git requests.\n"))
				return
			}
			args[1] = strings.Replace(args[1], "'", "", -1)

			cmd := exec.Command("git-shell", "-c", args[0]+" '"+os.Getenv("HOME")+"/git/"+args[1]+"'")
			pipeCmd(cmd, channel, channel.Stderr(), channel)
			cmd.Start()
			status, err := exitStatus(cmd.Wait())
			if err != nil {
				log.Println(err)
				return
			}

			if _, err := channel.SendRequest("exit-status", false, ssh.Marshal(&status)); err != nil {
				log.Println("sendExit, ", err)
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

func pipeCmd(cmd *exec.Cmd, stdout, stderr io.Writer, stdin io.Reader) {
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

func main() {
	config := &ssh.ServerConfig{
		PasswordCallback: func(c ssh.ConnMetadata, pass []byte) (*ssh.Permissions, error) {
			if c.User() == "git" && string(pass) == os.Getenv("GIT_PASSWD") {
				return nil, nil
			}
			return nil, fmt.Errorf("password rejected for %q", c.User())
		},
	}

	parseKeys(config)
	createServer(config)
}
