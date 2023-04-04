package main

import (
	"flag"
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

var (
	hostname   = flag.String("hostname", "vmproxy", "hostname")
	stateDir   = flag.String("state", "./state", "state directory")
	sshDir     = flag.String("ssh-dir", "./ssh", "SSH directory")
	libvirtLoc = flag.String("libvirt-loc", "/var/run/libvirt/libvirt-sock", "libvirt socket path or <ip addr>:<port>")
)

func main() {
	flag.Parse()

	if os.Getenv("VMPROXY_HOSTNAME") != "" {
		*hostname = os.Getenv("VMPROXY_HOSTNAME")
	}

	if os.Getenv("VMPROXY_STATE_DIR") != "" {
		*stateDir = os.Getenv("VMPROXY_STATE_DIR")
	}

	if os.Getenv("VMPROXY_SSH_DIR") != "" {
		*sshDir = os.Getenv("VMPROXY_SSH_DIR")
	}

	if os.Getenv("VMPROXY_LIBVIRT_LOC") != "" {
		*libvirtLoc = os.Getenv("VMPROXY_LIBVIRT_LOC")
	}

	var vmName, vncAddrPort string
	if len(os.Args) == 3 {
		vmName = os.Args[1]
		vncAddrPort = os.Args[2]
	}
	if os.Getenv("VMPROXY_VM_NAME") != "" {
		vmName = os.Getenv("VMPROXY_VM_NAME")
	}
	if os.Getenv("VMPROXY_VNC_ADDRPORT") != "" {
		vncAddrPort = os.Getenv("VMPROXY_VNC_ADDRPORT")
	}
	if vmName == "" || vncAddrPort == "" {
		log.Fatal("usage: vmproxy <vm name> <vnc addrport>")
	}

	must.Get(netip.ParseAddrPort(vncAddrPort))

	s := &tsnet.Server{
		Logf:     logger.Discard,
		Hostname: *hostname,
		Dir:      *stateDir,
	}

	var ln net.Listener

	ln = must.Get(s.Listen("tcp", ":22"))
	ssh := vmproxy.NewSSHServer(&vmproxy.SSHConfig{
		Domain:     vmName,
		SSHDir:     *sshDir,
		LibvirtLoc: *libvirtLoc,
	})
	go ssh.Serve(ln)

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
