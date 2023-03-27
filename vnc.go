package main

import (
	"context"
	"embed"
	"io/fs"
	"log"
	"net"
	"net/http"

	"github.com/evangwt/go-vncproxy"
	"golang.org/x/net/websocket"
	"tailscale.com/tsnet"
	"tailscale.com/util/must"
)

//go:embed noVNC
var f embed.FS

func vncProxy(s *tsnet.Server, tlsLn net.Listener) {
	p := vncproxy.New(&vncproxy.Config{
		TokenHandler: func(r *http.Request) (addr string, err error) {
			return vncAddrPort, nil
		},
	})

	mux := http.NewServeMux()

	sub := must.Get(fs.Sub(f, "noVNC"))
	st := must.Get(s.Up(context.TODO()))

	if len(st.CertDomains) == 0 {
		log.Fatal("no domains")
	}

	domain := st.CertDomains[0]

	mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			http.Redirect(w, r, "/vnc.html?host="+domain+"&port=&path=ws&encrypt=1&autoconnect=1", http.StatusFound)
			return
		}
		http.FileServer(http.FS(sub)).ServeHTTP(w, r)
	}))
	mux.Handle("/ws", websocket.Handler(p.ServeWS))
	log.Print("HTTPS server listening at https://" + domain)
	must.Do(http.Serve(tlsLn, mux))
}
