package main

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"github.com/valkey-io/valkey-go"
)

// mockRespServer starts an in-memory Redis RESP server for testing.
type mockRespServer struct {
	ln     net.Listener
	mu     sync.Mutex
	data   map[string][]byte
	closed chan struct{}
}

func newMockRespServer(t *testing.T) *mockRespServer {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}

	s := &mockRespServer{
		ln:     ln,
		data:   make(map[string][]byte),
		closed: make(chan struct{}),
	}

	go s.serve()
	return s
}

func (s *mockRespServer) Close() {
	close(s.closed)
	s.ln.Close()
}

func (s *mockRespServer) Addr() string {
	return s.ln.Addr().String()
}

func (s *mockRespServer) serve() {
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			select {
			case <-s.closed:
				return
			default:
				continue
			}
		}
		go s.handleConn(conn)
	}
}

func (s *mockRespServer) handleConn(conn net.Conn) {
	defer conn.Close()
	reader := bufio.NewReader(conn)

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "*") {
			continue
		}

		numArgs, _ := strconv.Atoi(line[1:])
		args := make([]string, 0, numArgs)
		for i := 0; i < numArgs; i++ {
			lenLine, err := reader.ReadString('\n')
			if err != nil {
				return
			}
			lenVal, _ := strconv.Atoi(strings.TrimSpace(lenLine)[1:])
			buf := make([]byte, lenVal+2) // value + \r\n
			if _, err := io.ReadFull(reader, buf); err != nil {
				return
			}
			args = append(args, string(buf[:lenVal]))
		}

		if len(args) == 0 {
			continue
		}

		cmd := strings.ToUpper(args[0])
		s.mu.Lock()
		switch cmd {
		case "CLUSTER":
			conn.Write([]byte("-ERR This instance has cluster support disabled\r\n"))
		case "PING":
			conn.Write([]byte("+PONG\r\n"))
		case "SET":
			if len(args) >= 3 {
				s.data[args[1]] = []byte(args[2])
				conn.Write([]byte("+OK\r\n"))
			}
		case "GET":
			if len(args) >= 2 {
				if val, ok := s.data[args[1]]; ok {
					conn.Write([]byte(fmt.Sprintf("$%d\r\n%s\r\n", len(val), val)))
				} else {
					conn.Write([]byte("$-1\r\n"))
				}
			}
		case "DEL":
			if len(args) >= 2 {
				delete(s.data, args[1])
				conn.Write([]byte(":1\r\n"))
			}
		default:
			conn.Write([]byte("+OK\r\n"))
		}
		s.mu.Unlock()
	}
}

func TestValkeyStoreSessionLifecycle(t *testing.T) {
	server := newMockRespServer(t)
	defer server.Close()

	client, err := valkey.NewClient(valkey.ClientOption{
		InitAddress: []string{server.Addr()},
		Dialer: net.Dialer{
			Timeout: 2 * time.Second,
		},
		AlwaysRESP2: true,
		DisableCache: true,
	})
	if err != nil {
		t.Fatalf("failed to create valkey client: %v", err)
	}
	defer client.Close()

	secret := []byte("very-secret-test-session-key-32b")
	store := NewValkeyStore(client, secret)
	store.Options(sessions.Options{
		Path:     "/",
		MaxAge:   3600,
		HttpOnly: true,
	})

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(sessions.Sessions("userSession", store))

	r.GET("/set", func(c *gin.Context) {
		session := sessions.Default(c)
		session.Set("user_id", "42")
		if err := session.Save(); err != nil {
			c.String(http.StatusInternalServerError, err.Error())
			return
		}
		c.String(http.StatusOK, "saved")
	})

	r.GET("/get", func(c *gin.Context) {
		session := sessions.Default(c)
		userID := session.Get("user_id")
		if userID == nil {
			c.String(http.StatusUnauthorized, "no session")
			return
		}
		c.String(http.StatusOK, fmt.Sprint(userID))
	})

	r.GET("/clear", func(c *gin.Context) {
		session := sessions.Default(c)
		session.Clear()
		session.Options(sessions.Options{MaxAge: -1, Path: "/"})
		_ = session.Save()
		c.String(http.StatusOK, "cleared")
	})

	// 1. Set session
	w1 := httptest.NewRecorder()
	req1, _ := http.NewRequest(http.MethodGet, "/set", nil)
	r.ServeHTTP(w1, req1)
	if w1.Code != http.StatusOK {
		t.Fatalf("expected /set 200, got %d: %s", w1.Code, w1.Body.String())
	}

	cookie := w1.Header().Get("Set-Cookie")
	if cookie == "" {
		t.Fatalf("expected Set-Cookie header in response")
	}

	// 2. Get session with cookie
	w2 := httptest.NewRecorder()
	req2, _ := http.NewRequest(http.MethodGet, "/get", nil)
	req2.Header.Set("Cookie", cookie)
	r.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("expected /get 200, got %d", w2.Code)
	}
	if w2.Body.String() != "42" {
		t.Fatalf("expected user_id 42, got %s", w2.Body.String())
	}

	// 3. Clear session
	w3 := httptest.NewRecorder()
	req3, _ := http.NewRequest(http.MethodGet, "/clear", nil)
	req3.Header.Set("Cookie", cookie)
	r.ServeHTTP(w3, req3)
	if w3.Code != http.StatusOK {
		t.Fatalf("expected /clear 200, got %d", w3.Code)
	}

	// 4. Verify session is gone
	w4 := httptest.NewRecorder()
	req4, _ := http.NewRequest(http.MethodGet, "/get", nil)
	req4.Header.Set("Cookie", cookie)
	r.ServeHTTP(w4, req4)
	if w4.Code != http.StatusUnauthorized {
		t.Fatalf("expected /get after clear to return 401, got %d", w4.Code)
	}
}
