package main

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	ss "github.com/dawei101/shadowsocks-go/shadowsocks"
	"golang.org/x/net/proxy"
	goproxy "gopkg.in/elazarl/goproxy.v1"
)

var (
	errAddrType      = errors.New("socks addr type not supported")
	errVer           = errors.New("socks version not supported")
	errMethod        = errors.New("socks only support 1 method now")
	errAuthExtraData = errors.New("socks authentication get extra data")
	errReqExtraData  = errors.New("socks request get extra data")
	errCmd           = errors.New("socks command not supported")
)

type TrafficListener struct {
	in  int64
	out int64
}

func (t *TrafficListener) WhenIn(len int) {
	atomic.AddInt64(&t.in, int64(len))
	if atomic.LoadInt64(&t.in) > 10485760 {
		go t.Sync()
	}
}

func (t *TrafficListener) WhenOut(len int) {
	atomic.AddInt64(&t.out, int64(len))
}

func (t *TrafficListener) Sync() {
	in, out := atomic.LoadInt64(&t.in), atomic.LoadInt64(&t.out)
	config, err := LoadConfig()
	log.Printf("Sync traffic, in: %d, out: %d", in, out)
	if err != nil {
		log.Printf("Load config failed, when sync traffic, error is: %v", err)
		return
	}
	config.AddTraffic(in, out)
	err = SaveConfig(config)
	if err != nil {
		log.Printf("Save config failed, when sync traffic, error is: %v", err)
		return
	}
	atomic.StoreInt64(&t.in, 0)
	atomic.StoreInt64(&t.out, 0)
}

var TrafficCounter *TrafficListener

const (
	socksVer5       = 5
	socksCmdConnect = 1
)

func init() {
	TrafficCounter = &TrafficListener{0, 0}
	rand.Seed(time.Now().Unix())
}

func fatalf(fmtStr string, args interface{}) {
	fmt.Fprintf(os.Stderr, fmtStr, args)
	os.Exit(-1)
}

func handShake(conn net.Conn) (err error) {
	const (
		idVer     = 0
		idNmethod = 1
	)
	// version identification and method selection message in theory can have
	// at most 256 methods, plus version and nmethod field in total 258 bytes
	// the current rfc defines only 3 authentication methods (plus 2 reserved),
	// so it won't be such long in practice

	buf := make([]byte, 258)

	var n int
	ss.SetReadTimeout(conn)
	// make sure we get the nmethod field
	if n, err = io.ReadAtLeast(conn, buf, idNmethod+1); err != nil {
		return
	}
	if buf[idVer] != socksVer5 {
		return errVer
	}
	nmethod := int(buf[idNmethod])
	msgLen := nmethod + 2
	if n == msgLen { // handshake done, common case
		// do nothing, jump directly to send confirmation
	} else if n < msgLen { // has more methods to read, rare case
		if _, err = io.ReadFull(conn, buf[n:msgLen]); err != nil {
			return
		}
	} else { // error, should not get extra data
		return errAuthExtraData
	}
	// send confirmation: version 5, no authentication required
	_, err = conn.Write([]byte{socksVer5, 0})
	return
}

func getRequest(conn net.Conn) (rawaddr []byte, host string, err error) {
	const (
		idVer   = 0
		idCmd   = 1
		idType  = 3 // address type index
		idIP0   = 4 // ip addres start index
		idDmLen = 4 // domain address length index
		idDm0   = 5 // domain address start index

		typeIPv4 = 1 // type is ipv4 address
		typeDm   = 3 // type is domain address
		typeIPv6 = 4 // type is ipv6 address

		lenIPv4   = 3 + 1 + net.IPv4len + 2 // 3(ver+cmd+rsv) + 1addrType + ipv4 + 2port
		lenIPv6   = 3 + 1 + net.IPv6len + 2 // 3(ver+cmd+rsv) + 1addrType + ipv6 + 2port
		lenDmBase = 3 + 1 + 1 + 2           // 3 + 1addrType + 1addrLen + 2port, plus addrLen
	)
	// refer to getRequest in server.go for why set buffer size to 263
	buf := make([]byte, 263)
	var n int
	ss.SetReadTimeout(conn)
	// read till we get possible domain length field
	if n, err = io.ReadAtLeast(conn, buf, idDmLen+1); err != nil {
		return
	}
	// check version and cmd
	if buf[idVer] != socksVer5 {
		err = errVer
		return
	}
	if buf[idCmd] != socksCmdConnect {
		err = errCmd
		return
	}

	reqLen := -1
	switch buf[idType] {
	case typeIPv4:
		reqLen = lenIPv4
	case typeIPv6:
		reqLen = lenIPv6
	case typeDm:
		reqLen = int(buf[idDmLen]) + lenDmBase
	default:
		err = errAddrType
		return
	}

	if n == reqLen {
		// common case, do nothing
	} else if n < reqLen { // rare case
		if _, err = io.ReadFull(conn, buf[n:reqLen]); err != nil {
			return
		}
	} else {
		err = errReqExtraData
		return
	}

	rawaddr = buf[idType:reqLen]

	if debug {
		switch buf[idType] {
		case typeIPv4:
			host = net.IP(buf[idIP0 : idIP0+net.IPv4len]).String()
		case typeIPv6:
			host = net.IP(buf[idIP0 : idIP0+net.IPv6len]).String()
		case typeDm:
			host = string(buf[idDm0 : idDm0+buf[idDmLen]])
		}
		port := binary.BigEndian.Uint16(buf[reqLen-2 : reqLen])
		host = net.JoinHostPort(host, strconv.Itoa(int(port)))
	}

	return
}

type ServerCipher struct {
	server string
	cipher *ss.Cipher
}

var servers struct {
	sync.RWMutex
	srvCipher []*ServerCipher
	failCnt   []int // failed connection count
}

func connectToServer(serverId int, rawaddr []byte, addr string) (remote *ss.Conn, err error) {
	se := servers.srvCipher[serverId]
	remote, err = ss.DialWithRawAddr(rawaddr, se.server, se.cipher.Copy())
	if err != nil {
		log.Println("error connecting to shadowsocks server:", err)
		const maxFailCnt = 30
		if servers.failCnt[serverId] < maxFailCnt {
			servers.failCnt[serverId]++
		}
		return nil, err
	}
	log.Printf("connected to %s via %s\n", addr, se.server)
	servers.failCnt[serverId] = 0
	return
}

// Connection to the server in the order specified in the config. On
// connection failure, try the next server. A failed server will be tried with
// some probability according to its fail count, so we can discover recovered
// servers.
func createServerConn(rawaddr []byte, addr string) (remote *ss.Conn, err error) {
	const baseFailCnt = 20
	n := len(servers.srvCipher)
	skipped := make([]int, 0)
	for i := 0; i < n; i++ {
		// skip failed server, but try it with some probability
		if servers.failCnt[i] > 0 && rand.Intn(servers.failCnt[i]+baseFailCnt) != 0 {
			skipped = append(skipped, i)
			continue
		}
		remote, err = connectToServer(i, rawaddr, addr)
		if err == nil {
			return
		}
	}
	// last resort, try skipped servers, not likely to succeed
	for _, i := range skipped {
		remote, err = connectToServer(i, rawaddr, addr)
		if err == nil {
			return
		}
	}
	return nil, err
}

func handleConnection(conn net.Conn, tl *TrafficListener) {
	if debug {
		log.Printf("socks connect from %s\n", conn.RemoteAddr().String())
	}
	closed := false
	defer func() {
		if !closed {
			conn.Close()
		}
	}()

	var err error = nil
	if err = handShake(conn); err != nil {
		log.Println("socks handshake:", err)
		return
	}
	rawaddr, addr, err := getRequest(conn)
	if err != nil {
		log.Println("error getting request:", err)
		return
	}
	// Sending connection established message immediately to client.
	// This some round trip time for creating socks connection with the client.
	// But if connection failed, the client will get connection reset error.
	_, err = conn.Write([]byte{0x05, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x08, 0x43})
	if err != nil {
		log.Println("send connection confirmation:", err)
		return
	}

	servers.RLock()
	remote, err := createServerConn(rawaddr, addr)
	servers.RUnlock()
	if err != nil || remote == nil {
		if len(servers.srvCipher) > 1 {
			log.Println("Failed connect to all avaiable shadowsocks server")
		}
		return
	}
	defer func() {
		if !closed {
			remote.Close()
		}
	}()
	remote.TrafficListener = tl

	go ss.PipeThenClose(conn, remote)
	ss.PipeThenClose(remote, conn)
	closed = true
	log.Println("closed connection to", addr)
}

func StartSS() {
	ln, err := net.Listen("tcp", SocksProxy)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("starting local socks5 server at %v ...\n", SocksProxy)
	for {
		conn, err := ln.Accept()
		if err != nil {
			log.Println("accept:", err)
			continue
		}
		go handleConnection(conn, TrafficCounter)
	}
}

func SetTunnels(tunnels []*SSTunnel) {
	if len(tunnels) == 0 {
		return
	}
	srvCipher := make([]*ServerCipher, len(tunnels))

	cipherCache := make(map[string]*ss.Cipher)
	for i, tunnel := range tunnels {
		cacheKey := tunnel.Method + "|" + tunnel.Password
		cipher, ok := cipherCache[cacheKey]
		if !ok {
			var err error
			cipher, err = ss.NewCipher(tunnel.Method, tunnel.Password)
			if err != nil {
				log.Fatal("Failed generating ciphers:", err)
			}
			cipherCache[cacheKey] = cipher
		}
		hostPort := fmt.Sprintf("%s:%s", tunnel.Ip, tunnel.Port)
		srvCipher[i] = &ServerCipher{hostPort, cipher}
	}
	log.Printf("Reset %d tunnels", len(tunnels))
	servers.Lock()
	servers.srvCipher = srvCipher
	servers.failCnt = make([]int, len(servers.srvCipher))
	servers.Unlock()
}

func StartHttpProxy() {
	parentProxy, err := url.Parse(fmt.Sprintf("socks5://%s", SocksProxy))

	if err != nil {
		fatalf("Failed to parse proxy URL: %v\n", err)
	}

	tbDialer, err := proxy.FromURL(parentProxy, proxy.Direct)
	if err != nil {
		fatalf("Failed to obtain proxy dialer: %v\n", err)
	}
	server := goproxy.NewProxyHttpServer()
	server.Tr = &http.Transport{Dial: tbDialer.Dial}
	log.Printf("start http proxy at: %s", HttpProxy)
	err = http.ListenAndServe(HttpProxy, server)
	if err != nil {
		fatalf("Failed to start http proxy: %v\n", err)
	}
}

func MakeProxyClient() *http.Client {
	proxyUrl, _ := url.Parse(fmt.Sprintf("http://%s", HttpProxy))
	return &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxyUrl)}, Timeout: 10 * time.Second}
}

func TestProxyOk() (bool, error) {
	urla := "https://www.google.com/favicon.ico"
	urlb := "http://www.qq.com/favicon.ico"
	client := MakeProxyClient()
	_, erra := client.Get(urla)
	_, errb := http.Get(urlb)

	if erra != nil {
		if errb != nil {
			//network error
			return false, errb
		} else {
			//tongshe proxy could not use
			return false, nil
		}
	}
	return true, nil
}
