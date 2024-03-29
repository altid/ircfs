package main

import (
	"flag"
	"log"
	"os"

	"github.com/altid/ircfs"
)

var (
	srv   = flag.String("s", "irc", "name of service")
	addr  = flag.String("a", "127.0.0.1:12345", "listening address")
	mdns  = flag.Bool("m", false, "enable mDNS broadcast of service")
	debug = flag.Bool("d", false, "enable debug printing")
	ssh   = flag.Bool("x", false, "enable ssh listener (default \"9p\")")
	ldir  = flag.Bool("l", false, "enable logging for main buffers")
	setup = flag.Bool("conf", false, "run configuration setup")
)

func main() {
	flag.Parse()
	if flag.Lookup("h") != nil {
		flag.Usage()
		os.Exit(1)
	}
	if *setup {
		if e := ircfs.CreateConfig(*srv, *debug); e != nil {
			log.Fatal(e)
		}
		os.Exit(0)
	}
	irc, err := ircfs.Register(*ssh, *ldir, *addr, *srv, *debug)
	if err != nil {
		log.Fatal(err)
	}
	defer irc.Cleanup()
	if *mdns {
		if e := irc.Broadcast(); e != nil {
			log.Fatal(e)
		}
	}
	if e := irc.Run(); e != nil {
		log.Fatal(e)
	}
	os.Exit(0)
}
