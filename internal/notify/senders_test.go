package notify

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSlackEscapesUserText(t *testing.T) {
	var received string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]string
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		received = payload["text"]
	}))
	defer server.Close()
	err := (slackSender{server.Client()}).Send(context.Background(), map[string]string{"webhook_url": server.URL}, Event{Title: "<!here> & test", Message: "<https://evil|click>"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(received, "<") || received != "*&lt;!here&gt; &amp; test*\n&lt;https://evil|click&gt;" {
		t.Fatalf("unsafe Slack payload: %s", received)
	}
}
func TestSMTPRequiresTLSBeforeSendingMail(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	commands := make(chan string, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		conn.SetDeadline(time.Now().Add(2 * time.Second))
		fmt.Fprint(conn, "220 test ESMTP\r\n")
		reader := bufio.NewReader(conn)
		reader.ReadString('\n')
		fmt.Fprint(conn, "250 test\r\n")
		line, _ := reader.ReadString('\n')
		commands <- line
	}()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err = sendMail(ctx, listener.Addr().String(), "localhost", nil, "a@test", []string{"b@test"}, []byte("secret"), true)
	if err == nil || !strings.Contains(err.Error(), "STARTTLS") {
		t.Fatalf("expected TLS refusal: %v", err)
	}
	if line := <-commands; strings.HasPrefix(line, "MAIL") || strings.HasPrefix(line, "AUTH") {
		t.Fatalf("sent before TLS: %s", line)
	}
}
