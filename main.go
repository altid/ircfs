package main

import (
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/lrstanley/girc"
	"github.com/ubqt-systems/ubqtlib"
)

var (
	addr    = flag.String("a", ":4567", "Port to listen on")
	conf    = flag.String("c", "irc.ini", "Configuration file")
	inPath  = flag.String("p", "~/irc", "Path for file system")
	debug   = flag.Bool("d", false, "Enable debugging output")
	verbose = flag.Bool("v", false, "Enable verbose output")
)

// State - holds server session
type State struct {
	//track current client per connection, as well as current channel
	irc     map[string]*girc.Client
	channel map[string]*girc.Channel
	tab     []byte
	input   []byte
}

// ClientWrite - Handle writes on ctl, input to send to channel/mutate program state
func (st *State) ClientWrite(filename string, client string, data []byte) (n int, err error) {
	switch filename {
	case "input":
		n, err = st.handleInput(data, client)
	case "ctl":
		n, err = st.handleCtl(data, client)
	default:
		err = errors.New("permission denied")
	}
	return
}

// ClientRead - Return formatted strings for various files
func (st *State) ClientRead(filename string, client string) (buf []byte, err error) {
	// Calls may error, pass that back as required
	switch filename {
	case "input":
		return st.input, nil
	case "ctl":
		buf, err = st.ctl(client)
	case "status":
		buf, err = st.status(client)
	case "sidebar":
		buf, err = st.sidebar(client)
	case "tabs":
		buf, err = st.tabs(client)
	case "main":
		buf, err = st.buff(client)
	case "title":
		buf, err = st.title(client)
	default:
		err = errors.New("permission denied")
	}
	return
}

// ClientConnect - called when client connects
func (st *State) ClientConnect(client string) {
	st.channel[client] = st.channel["default"]
	st.irc[client] = st.irc["default"]
}

// ClientDisconnect - called when client disconnects
func (st *State) ClientDisconnect(client string) {
	delete(st.channel, client)
	delete(st.irc, client)
}

func main() {
	flag.Parse()
	if flag.Lookup("h") != nil {
		flag.Usage()
		os.Exit(1)
	}
	st := &State{}
	st.irc = make(map[string]*girc.Client)
	st.channel = make(map[string]*girc.Channel)
	srv := ubqtlib.NewSrv()
	if *debug {
		srv.Debug()
	}
	if *verbose {
		srv.Verbose()
	}
	err := st.initialize(srv)
	if err != nil {
		fmt.Printf("Err %s", err)
		os.Exit(1)
	}
	err = srv.Loop(st)
	if err != nil {
		fmt.Printf("Err %s", err)
		os.Exit(1)
	}
}
