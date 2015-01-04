// ************************************************************************** //
//                                                                            //
//                                                        :::      ::::::::   //
//   git_server.go                                      :+:      :+:    :+:   //
//                                                    +:+ +:+         +:+     //
//   By: niccheva <niccheva@student.42.fr>          +#+  +:+       +#+        //
//                                                +#+#+#+#+#+   +#+           //
//   Created: 2015/01/04 18:39:27 by niccheva          #+#    #+#             //
//   Updated: 2015/01/04 19:18:40 by niccheva         ###   ########.fr       //
//                                                                            //
// ************************************************************************** //

package main

import (
	"fmt"
	"io/ioutil"
	"log"
	"os"

	"code.google.com/p/go.crypto/ssh"
)

func parseKeys(conf *ssh.ServerConfig) {
	privateBytes, err := ioutil.ReadFile(os.Getenv("HOME") + "/.ssh/id_rsa")
	if err != nil {
		log.Fatal("Failed to load private key:", err)
	}

	private, err := ssh.ParsePrivateKey(privateBytes)
	if err != nil {
		log.Fatal("Failed to parse private key:", err)
	}

	conf.AddHostKey(private)
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
}
