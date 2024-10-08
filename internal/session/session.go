package session

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"strings"

	"github.com/altid/irc/internal/format"
	"github.com/altid/libs/markup"
	"github.com/altid/libs/service/commander"
	"github.com/altid/libs/service/controller"
	irc "gopkg.in/irc.v4"
)

type ctlItem int

const (
	ctlJoin ctlItem = iota
	ctlPart
	ctlStart
	ctlEvent
	ctlMsg
	ctlCommand
	ctlInput
	ctlRun
	ctlSucceed
	ctlTopic
	ctlErr
	ctlQuit
	ctcpMsg
)

type Session struct {
	Client   *irc.Client
	ctx      context.Context
	cancel   context.CancelFunc
	conn     net.Conn
	conf     irc.ClientConfig
	ctrl     controller.Controller
	Defaults *Defaults
	Verbose  bool
	debug    func(ctlItem, ...any)
}

type Defaults struct {
	Address string       `altid:"address,prompt:IP Address of IRC server you wish to connect to"`
	SSL     string       `altid:"ssl,prompt:SSL mode,pick:none|simple|certificate"`
	Port    int          `altid:"port,no_prompt"`
	Filter  string       `altid:"filter,no_prompt"`
	Nick    string       `altid:"nick,prompt:Enter your IRC nickname (this is what will be shown on messages you send)"`
	User    string       `altid:"user,no_prompt"`
	Name    string       `altid:"name,no_prompt"`
	Buffs   string       `altid:"buffs,no_prompt"`
	TLSCert string       `altid:"tlscert,no_prompt"`
	TLSKey  string       `altid:"tlskey,no_prompt"`
}

func (s *Session) Parse(ctx context.Context) {
	s.debug = func(ctlItem, ...any) {}
	s.ctx, s.cancel = context.WithCancel(ctx)

	s.conf = irc.ClientConfig{
		User:    s.Defaults.User,
		Nick:    s.Defaults.Nick,
		Name:    s.Defaults.Name,
		Handler: handlerFunc(s),
	}

	if s.Verbose {
		s.debug = ctlLogging
	}
}

// Future, multiuser
func (s *Session) Connect(Username string) error {
	// We can check blacklists here, etc
	return nil
}

func (s *Session) Run(c controller.Controller, cmd *commander.Command) error {
	s.debug(ctlMsg, cmd)
	switch cmd.Name {
	case "a", "act", "action", "me":
		if len(cmd.Args) < 1 {
			e := errors.New("no action entered")
			s.debug(ctlErr, e)
			return e
		}
		line := strings.Join(cmd.Args[1:], " ")
		if e := action(s.conn, s.conf.Name, cmd.Args[0], line); e != nil {
			s.debug(ctlErr, e)
			return e
		}
	case "msg", "query":
		if len(cmd.Args) < 1 {
			e := errors.New("no user specified")
			s.debug(ctlErr, e)
			return e
		}
		if e := c.CreateBuffer(cmd.Args[0]); e != nil {
			s.debug(ctlErr, e)
			return e
		}
		
		if len(cmd.Args) > 1 {
			line := strings.Join(cmd.Args[1:], " ")
			if e := pm(s.conn, s.conf.Name, line); e != nil {
				s.debug(ctlErr, e)
				return e
			}
		}
	case "nick":
		fmt.Fprintf(s.conn, "NICK %s\n", cmd.Args[0])
	case "close":
		// IRC buffers do not allow spaces
		s.debug(ctlPart, cmd.Args[0])
		if e := c.DeleteBuffer(cmd.Args[0]); e != nil {
			s.debug(ctlErr, e)
			return e
		}

		_, err := fmt.Fprintf(s.conn, "PART %s\n", cmd.Args[0])
		if err != nil {
			s.debug(ctlErr, err)
			return err
		}
		return nil
	case "open":
		s.debug(ctlJoin, cmd.Args[0])
		// This is a bit fragile, make sure we're not looping here
		if cmd.Args[0][0] == '#' {
			if _, e := fmt.Fprintf(s.conn, "JOIN %s\n", cmd.Args[0]); e != nil {
				s.debug(ctlErr, e)
				return e
			}
		}
		s.debug(ctlSucceed, "join")
		return nil
	case "ready":
		//make the buffer
		if e := c.CreateBuffer(cmd.Args[0]); e != nil {
			s.debug(ctlErr, e)
			return e
		}
	default:
		e := fmt.Errorf("unsupported command %s", cmd.Name)
		s.debug(ctlErr, e)
		return e
	}

	s.debug(ctlSucceed, cmd)
	return nil
}

func (s *Session) Quit() {
	s.cancel()
}

func (s *Session) Ctl(c *commander.Command) {
	fmt.Print(c.Bytes())
}
func (s *Session) Input(b []byte) {
	fmt.Print(b)
}

// input is always sent down raw to the server
func (s *Session) Handle(bufname string, l *markup.Lexer) error {
	data, err := format.Input(l)
	if l == nil {
		e := errors.New("provided lexer was nil")
		s.debug(ctlErr, e)
		return e
	}
	s.debug(ctlInput, data, bufname)
	if err != nil {
		s.debug(ctlErr, err)
		return err
	}

	if _, e := fmt.Fprintf(s.conn, ":%s PRIVMSG %s :%s\n", s.conf.Name, bufname, data); e != nil {
		s.debug(ctlErr, e)
		return e
	}
	m := &msg{
		// Some clients can send whitespace on the end, make sure we clear it out
		data: strings.TrimRight(string(data), "\n\r"),
		from: s.conf.Nick,
		buff: bufname,
		fn:   fself,
	}
	fileWriter(s.ctrl, m)
	s.debug(ctlSucceed, "input")
	return nil
}

func (s *Session) Start(c controller.Controller) error {
	if err := s.connect(s.ctx); err != nil {
		s.debug(ctlErr, err)
		return err
	}

	c.CreateBuffer("server")
	s.ctrl = c
	s.Client = irc.NewClient(s.conn, s.conf)
	log.Println("In start")
	return s.Client.Run()
}

func (s *Session) Command(cmd *commander.Command) error {
	return s.Run(s.ctrl, cmd)
}

func (s *Session) connect(ctx context.Context) error {
	var tlsConfig *tls.Config

	s.debug(ctlStart, s.Defaults.Address, s.Defaults.Port)
	dialString := fmt.Sprintf("%s:%d", s.Defaults.Address, s.Defaults.Port)
	dialer := &net.Dialer{}

	conn, err := dialer.DialContext(ctx, "tcp", dialString)
	if err != nil {
		s.debug(ctlErr, err)
		return err
	}

	switch s.Defaults.SSL {
	case "none":
		s.conn = conn
		s.debug(ctlRun)
		return nil
	case "simple":
		tlsConfig = &tls.Config{
			ServerName:         dialString,
			InsecureSkipVerify: true,
		}
	case "certificate":
		cert, err := tls.LoadX509KeyPair(s.Defaults.TLSCert, s.Defaults.TLSKey)
		if err != nil {
			s.debug(ctlErr, err)
			return err
		}

		tlsConfig = &tls.Config{
			Certificates: []tls.Certificate{
				cert,
			},
			ServerName: dialString,
		}
	}

	tlsconn := tls.Client(conn, tlsConfig)
	if e := tlsconn.Handshake(); e != nil {
		s.debug(ctlErr, e)
		return e
	}

	s.conn = tlsconn
	s.debug(ctlRun)
	log.Println("In connect")

	return nil
}

func ctlLogging(ctl ctlItem, args ...any) {
	l := log.New(os.Stdout, "irc ", 0)

	switch ctl {
	case ctlSucceed:
		l.Printf("%s succeeded\n", args[0])
	case ctlJoin:
		l.Printf("join: target=\"%s\"\n", args[0])
	case ctlStart:
		l.Printf("start: addr=\"%s\", port=%d\n", args[0], args[1])
	case ctlRun:
		l.Println("connected")
	case ctlPart:
		l.Printf("part: target=\"%s\"\n", args[0])
	case ctlEvent:
		l.Printf("event: data=\"%s\"\n", args[0])
	case ctlInput:
		l.Printf("input: data=\"%s\" bufname=\"%s\"", args[0], args[1])
	case ctlCommand:
		m := args[0].(*commander.Command)
		l.Printf("command name=\"%s\" heading=\"%d\" args=\"%s\" from=\"%s\"", m.Name, m.Heading, m.Args, m.From)
	case ctlMsg:
		m := args[0].(*commander.Command)
		line := strings.Join(m.Args, " ")
		l.Printf("%s: data=\"%s\"\n", m.Name, line)
	case ctlErr:
		l.Printf("error: err=\"%v\"\n", args[0])
	case ctlTopic:
		m := args[0].(*irc.Message)
		l.Printf("topic: data=\"%s\"", m.Params[1])
	case ctcpMsg:
		m := args[0].(*irc.Message)
		l.Printf("ctcp: name=\"%s\" prefix=\"%s\" params=\"%v\"\n", m.Name, m.Prefix, m.Params)
	default:
		l.Printf("%v\n", args)
	}
}
