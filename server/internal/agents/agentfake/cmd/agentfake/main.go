// Command agentfake runs an isolated loopback protocol oracle for Python/Go tests.
package main

import (
	"configflow/internal/agents/agentfake"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
)

func main() {
	listen := flag.String("listen", "127.0.0.1:0", "loopback listener")
	script := flag.String("script", "", "Options JSON file")
	flag.Parse()
	var options agentfake.Options
	if *script != "" {
		raw, e := os.ReadFile(*script)
		if e != nil {
			panic(e)
		}
		if e = json.Unmarshal(raw, &options); e != nil {
			panic(e)
		}
	}
	ln, e := net.Listen("tcp", *listen)
	if e != nil {
		panic(e)
	}
	host, _, e := net.SplitHostPort(ln.Addr().String())
	if e != nil || !net.ParseIP(host).IsLoopback() {
		ln.Close()
		panic("loopback required")
	}
	fmt.Println("http://" + ln.Addr().String())
	_ = http.Serve(ln, agentfake.New(options))
}
