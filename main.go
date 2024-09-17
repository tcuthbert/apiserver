package main

import (
	"flag"
	"fmt"
	"os"

	srv "github.com/tcuthbert/apiserver/webserver"
)

var (
	apiBaseURL = "https://api.github.com/"
	listenAddr = ":5000"
)

func main() {
	flag.StringVar(&listenAddr, "listen-addr", listenAddr, "server listen address")
	flag.Parse()

	if err := srv.Start(&listenAddr, apiBaseURL + `users/tcuthbert/repos`); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to start server: %s\n", err)
		os.Exit(1)
	}

	os.Exit(0)
}
