package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"strconv"
	"sync"
	"time"
)

type ServerState struct {
	Whitelist []*net.IPNet
}

func NewServerState() (s *ServerState) {
	s = new(ServerState)

	return
}

func (s *ServerState) SetWhitelist(value []*net.IPNet) {
	s.Whitelist = value
}

type Server struct {
	ctx       context.Context
	ctxCancel context.CancelCauseFunc
	state     *ServerState
	stateLock sync.RWMutex
}

func NewServer() (s *Server) {
	s = new(Server)

	s.ctx, s.ctxCancel = context.WithCancelCause(context.Background())
	s.state = NewServerState()
	s.stateLock = sync.RWMutex{}

	return
}

func (s *Server) ContainsAddr(addr *net.TCPAddr) bool {
	s.stateLock.RLock()
	defer s.stateLock.RUnlock()

	for _, cidr := range s.state.Whitelist {
		if cidr.Contains(addr.IP) {
			return true
		}
	}

	return false
}

func (s *Server) RoutineUpdate() {
	ticker := time.NewTicker(time.Minute * 5)

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			break
		}

		s.Update()
	}

}

func (s *Server) Update() {
	slog.Info("pulling cloudflare ips")
	data, err := PullCloudflareApiIps()
	if err != nil {
		slog.Error("pulling cloudflare ips fail",
			slog.Any("err", err.Error()))
		return
	}

	cidrList := make([]*net.IPNet, 0)
	addrList := append(data.Ipv4CidrList, data.Ipv6CidrList...)
	for _, addr := range addrList {
		_, ipNet, err := net.ParseCIDR(addr)
		if err != nil {
			slog.Error("parsing cidr fail",
				slog.Any("err", err.Error()))
			return
		}

		cidrList = append(cidrList, ipNet)
	}

	s.stateLock.Lock()
	defer s.stateLock.Unlock()

	slog.Info("updating whitelist")
	s.state.Whitelist = cidrList
}

func (s *Server) Run() {
	go s.RoutineUpdate()
	go s.Update()

	time.Sleep(time.Second * 10)
	go s.StartServer()

	<-s.ctx.Done()
}

func (s *Server) Stop(err error) {
	s.ctxCancel(err)
}

func (s *Server) StartServer() {
	var (
		addr     string
		port     int
		listener net.Listener
		err      error
	)

	addr = os.Getenv("RTNL_DOOR_ADDR")
	port, err = strconv.Atoi(os.Getenv("RTNL_DOOR_PORT"))
	if err != nil {
		panic(err)
	}

	listener, err = net.Listen("tcp", fmt.Sprintf("%s:%d", addr, port))
	if err != nil {
		panic(err)
	}
	defer listener.Close()

	for {
		var (
			conn net.Conn
		)

		conn, err = listener.Accept()
		if err != nil {
			slog.Warn("conn accept fail",
				slog.Any("err", err.Error()))
			continue
		}
		slog.Info("new client",
			slog.Any("remote", conn.RemoteAddr().String()))

		addrTcp, ok := conn.RemoteAddr().(*net.TCPAddr)
		if !ok {
			slog.Warn("could not cast to *net.TCPAddr")
			conn.Close()
			continue
		}

		if !s.ContainsAddr(addrTcp) {
			slog.Warn("client addr not in whitelist")
			conn.Close()
			continue
		}

		go s.HandleConn(conn)
	}
}

func (s *Server) HandleConn(client net.Conn) {
	var (
		remote net.Conn
		err    error
	)

	defer client.Close()

	slog.Debug("dial remote")
	remote, err = net.Dial("tcp", os.Getenv("RTNL_DOOR_REMOTE"))
	if err != nil {
		slog.Error("dial remote fail",
			slog.Any("err", err.Error()))
		return
	}

	defer remote.Close()

	ctx, cancel := context.WithCancelCause(context.Background())

	pump := func(ctx context.Context, cancel context.CancelCauseFunc, connA, connB net.Conn) {
		var (
			size int
			err  error
		)

		buf := make([]byte, 8192)

		for {
			select {
			case <-s.ctx.Done():
				return
			case <-ctx.Done():
				return
			default:
				{
					size, err = connA.Read(buf)
					if err != nil {
						cancel(err)
						return
					}

					_, err = connB.Write(buf[:size])
					if err != nil {
						cancel(err)
						return
					}
				}
			}
		}
	}

	go pump(ctx, cancel, client, remote)
	go pump(ctx, cancel, remote, client)

	<-ctx.Done()
}
