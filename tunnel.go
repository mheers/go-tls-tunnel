// refactored from https://gist.githubusercontent.com/cs8425/a742349a55596f1b251a/raw/d880371df13ebcd1ffdddcba92382a4d8687a818/tcp2tls_client.go

package tunnel

import (
	"crypto/tls"
	"errors"
	"net"
	"strconv"

	"github.com/sirupsen/logrus"
)

// Tunnel describes a tunnel
type Tunnel struct {
	localAddr          string
	remoteAddr         string
	remoteHost         string
	maxConn            int16
	insecureSkipVerify bool
	listener           *net.TCPListener
	stop               bool
}

// NewTunnel creates a new tunnel
func NewTunnel(remoteHost string, remotePort uint16, localHost string, localPort uint16, maxConn int16, insecureSkipVerify bool) *Tunnel {
	t := &Tunnel{
		localAddr:          localHost + ":" + strconv.Itoa(int(localPort)),   //  "127.0.0.1:5588",
		remoteAddr:         remoteHost + ":" + strconv.Itoa(int(remotePort)), // "example.com:4444",
		remoteHost:         remoteHost,
		maxConn:            maxConn,
		insecureSkipVerify: insecureSkipVerify,
		stop:               false,
	}
	return t
}

func (t *Tunnel) proxyConn(conn net.Conn) {
	defer conn.Close()

	rAddr, err := net.ResolveTCPAddr("tcp", t.remoteAddr)
	if err != nil {
		logrus.Print(err)
	}

	conf := &tls.Config{
		InsecureSkipVerify: t.insecureSkipVerify,
		ServerName:         t.remoteHost,
	}

	//rConn, err := net.DialTCP("tcp", nil, rAddr)
	rConn, err := tls.Dial("tcp", rAddr.String(), conf)
	if err != nil {
		logrus.Print(err)
		return
	}
	defer rConn.Close()

	//	logrus.Printf("remoteAddr connected: %v\n", rAddr.String())

	t.pipe(conn, rConn)
	//	logrus.Printf("proxyConn end: %v -> %v\n", conn.RemoteAddr(), rConn.RemoteAddr())
}

func (t *Tunnel) chanFromConn(conn net.Conn) chan []byte {
	c := make(chan []byte)

	go func() {
		b := make([]byte, 1024)

		for {
			n, err := conn.Read(b)
			if n > 0 {
				res := make([]byte, n)
				// Copy the buffer so it doesn't get changed while read by the recipient.
				copy(res, b[:n])
				c <- res
			}
			if err != nil {
				c <- nil
				break
			}
		}
	}()

	return c
}

func (t *Tunnel) pipe(conn1 net.Conn, conn2 net.Conn) {
	chan1 := t.chanFromConn(conn1)
	chan2 := t.chanFromConn(conn2)

	for {
		select {
		case b1 := <-chan1:
			if b1 == nil {
				return
			}
			conn2.Write(b1)

		case b2 := <-chan2:
			if b2 == nil {
				return
			}
			conn1.Write(b2)
		}
	}
}

// Close closes the connection
func (t *Tunnel) Close() error {
	if t.listener != nil {
		t.stop = true
		return t.listener.Close()
	}
	return errors.New("listener is nil")
}

// StartTunnel starts the tunnel
func (t *Tunnel) StartTunnel() error {
	logrus.Printf("Listening: %v -> %v\n\n", t.localAddr, t.remoteAddr)

	addr, err := net.ResolveTCPAddr("tcp", t.localAddr)
	if err != nil {
		return err
	}

	listener, err := net.ListenTCP("tcp", addr)
	if err != nil {
		return err
	}
	t.listener = listener
	// defer listener.Close()

	var i int16 = 0
	count := make(chan int16, 1)
	go func() {
		for !t.stop {
			logrus.Printf("wait accepted...\n")
			conn, err := listener.AcceptTCP()
			if err != nil {
				logrus.Print(err)
			}
			// t.conn = conn
			// defer conn.Close()
			//go proxyConn(conn);

			//logrus.Printf("accepted: %v\n", conn.RemoteAddr())
			select {
			case t := <-count:
				i += t
				//				print("received ", i, "\n")

			default:

			}
			if i < t.maxConn && conn != nil {
				i++
				ra := conn.RemoteAddr()
				logrus.Printf("[%v/%v]accepted: %v\n", i, t.maxConn, ra)

				// Create a new goroutine which will call the connection handler and  then free up the space.
				go func(connection net.Conn) {
					t.proxyConn(connection)
					//				logrus.Printf("[%v/%v]Closed connection from %s\r\n", i, maxConn, connection.RemoteAddr())
					logrus.Printf("Closed connection from %s\r\n", connection.RemoteAddr())
					select {
					case t := <-count:
						count <- t - 1

					default:
						count <- -1
					}
				}(conn)
			} else {
				if conn != nil {
					conn.Close()
				}
				logrus.Printf("closed: %v\n", i)
			}
		}
	}()
	return nil
}
