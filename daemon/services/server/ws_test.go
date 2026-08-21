package server

import (
	"bytes"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"

	"unbalance/daemon/domain"
)

func TestWebsocketReadIsBenignClose(t *testing.T) {
	if !websocketReadIsBenignClose(nil) {
		t.Fatal("nil must be treated as a clean websocket return")
	}
	if !websocketReadIsBenignClose(&websocket.CloseError{Code: websocket.CloseGoingAway, Text: "going away"}) {
		t.Fatal("close 1001 (going away) is an expected browser refresh")
	}
	if !websocketReadIsBenignClose(&websocket.CloseError{Code: websocket.CloseNormalClosure}) {
		t.Fatal("close 1000 is a normal websocket close")
	}
	if websocketReadIsBenignClose(fmt.Errorf("read failed")) {
		t.Fatal("unexpected read errors must not be classified as benign")
	}
}

func TestSkipGzipForWebsocket(t *testing.T) {
	e := echo.New()

	wsReq := httptest.NewRequest(http.MethodGet, "/ws", nil)
	wsReq.Header.Set("Upgrade", "websocket")
	if !skipGzipForWebsocket(e.NewContext(wsReq, httptest.NewRecorder())) {
		t.Fatal("gzip must skip websocket upgrades")
	}

	apiReq := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	if skipGzipForWebsocket(e.NewContext(apiReq, httptest.NewRecorder())) {
		t.Fatal("gzip must still apply to ordinary HTTP API routes")
	}
}

func TestWsHandlerGoingAwayDoesNotWriteHTTPAfterUpgrade(t *testing.T) {
	s := &Server{ctx: &domain.Context{}}
	e := echo.New()
	e.HideBanner = true
	e.HidePort = true
	e.Use(middleware.GzipWithConfig(middleware.GzipConfig{Skipper: skipGzipForWebsocket}))
	e.GET("/ws", s.wsHandler)

	var logBuf bytes.Buffer
	srv := httptest.NewUnstartedServer(e)
	srv.Config.ErrorLog = log.New(&logBuf, "", 0)
	srv.Start()
	t.Cleanup(srv.Close)

	header := http.Header{}
	header.Set("Origin", srv.URL)
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, header)
	if err != nil {
		t.Fatalf("websocket dial: %v", err)
	}

	deadline := time.Now().Add(time.Second)
	if err := conn.WriteControl(
		websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.CloseGoingAway, ""),
		deadline,
	); err != nil {
		t.Fatalf("send close 1001: %v", err)
	}
	_ = conn.Close()
	time.Sleep(200 * time.Millisecond)

	logs := logBuf.String()
	if strings.Contains(logs, "hijacked") || strings.Contains(logs, "WriteHeader") {
		t.Fatalf("browser refresh close wrote to the hijacked HTTP connection: %s", logs)
	}
}
