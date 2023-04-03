package main

import (
	"log"
	"net"
	"net/http"
	"net/netip"
	"os"

	"tailscale.com/tsnet"
	"tailscale.com/types/logger"
	"tailscale.com/util/must"

	"github.com/shayne/vmproxy"
)

func main() {
	if len(os.Args) != 3 {
		log.Fatal("usage: vmproxy <vm name> <vnc addrport>")
	}

	vmName := os.Args[1]
	vncAddrPort := os.Args[2]

	must.Get(netip.ParseAddrPort(vncAddrPort))

	s := &tsnet.Server{
		Logf:     logger.Discard,
		Hostname: "vmproxy",
		Dir:      "./state",
	}

	var ln net.Listener

	ln = must.Get(s.Listen("tcp", ":22"))
	go vmproxy.SSHServer(ln, vmName)

	ln = must.Get(s.Listen("tcp", ":80"))
	go promoteHTTPS(ln)

	ln = must.Get(s.ListenTLS("tcp", ":443"))
	vmproxy.VNCProxy(s, ln, vncAddrPort)
}

func promoteHTTPS(ln net.Listener) {
	log.Print("HTTP server listening on port 80 (redirecting to HTTPS)")
	must.Do(http.Serve(ln, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://"+r.Host+r.RequestURI, http.StatusFound)
	})))
}
