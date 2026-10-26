// A kjak feather plugin for the tests (protocol v1): what it does is chosen
// by the setting "mode" of the request. Built by the tests with go build.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

type request struct {
	ProtocolVersion int               `json:"protocol_version"`
	Feather         string            `json:"feather"`
	Attempt         int               `json:"attempt"`
	IdempotencyKey  string            `json:"idempotency_key"`
	Message         map[string]string `json:"message"`
	Settings        map[string]string `json:"settings"`
	Secrets         map[string]string `json:"secrets"`
}

func respond(v any) {
	_ = json.NewEncoder(os.Stdout).Encode(v)
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "child" {
		// a child that outlives its parent unless its group is killed
		_ = os.WriteFile(os.Args[2], []byte(fmt.Sprint(os.Getpid())), 0o600)
		time.Sleep(time.Hour)
		return
	}
	in, err := io.ReadAll(os.Stdin)
	if err != nil {
		os.Exit(9)
	}
	var req request
	if err := json.Unmarshal(in, &req); err != nil {
		fmt.Fprintln(os.Stderr, "bad request:", err)
		os.Exit(8)
	}
	s := req.Settings
	if s["log"] != "" {
		f, _ := os.OpenFile(s["log"], os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		fmt.Fprintf(f, "%d %s\n", req.Attempt, req.IdempotencyKey)
		f.Close()
	}
	switch s["mode"] {
	case "ok", "":
		respond(map[string]any{"protocol_version": 1, "status": "delivered", "acceptance": "confirmed", "status_code": 202})
	case "dump":
		cwd, _ := os.Getwd()
		dump := map[string]any{"request": json.RawMessage(in), "args": os.Args, "env": os.Environ(), "cwd": cwd}
		b, _ := json.Marshal(dump)
		_ = os.WriteFile(s["dump"], b, 0o600)
		respond(map[string]any{"protocol_version": 1, "status": "delivered"})
	case "rejected":
		respond(map[string]any{"protocol_version": 1, "status": "failed", "acceptance": "rejected", "retryable": true, "retry_after_ms": 5,
			"error": map[string]any{"code": "rate_limited", "message": "slow down"}})
	case "permanent":
		respond(map[string]any{"protocol_version": 1, "status": "failed", "acceptance": "rejected", "status_code": 400,
			"error": map[string]any{"code": "made_up_code", "message": "bad \x1b[31mrequest"}})
	case "unknown":
		respond(map[string]any{"protocol_version": 1, "status": "failed", "acceptance": "unknown", "error": map[string]any{"message": "lost"}})
	case "echo-secret":
		var all []string
		for _, v := range req.Secrets {
			all = append(all, v)
		}
		fmt.Fprintln(os.Stderr, "debug: secrets", strings.Join(all, ","))
		respond(map[string]any{"protocol_version": 1, "status": "failed", "acceptance": "rejected",
			"error": map[string]any{"code": "rejected", "message": "token " + strings.Join(all, ",") + " refused"}})
	case "badjson":
		fmt.Println("this is not json")
	case "empty":
	case "twojson":
		respond(map[string]any{"protocol_version": 1, "status": "delivered"})
		respond(map[string]any{"protocol_version": 1, "status": "delivered"})
	case "version2":
		respond(map[string]any{"protocol_version": 2, "status": "delivered"})
	case "noversion":
		respond(map[string]any{"status": "delivered"})
	case "inconsistent":
		respond(map[string]any{"protocol_version": 1, "status": "delivered", "acceptance": "rejected"})
	case "extra":
		respond(map[string]any{"protocol_version": 1, "status": "delivered", "future_field": map[string]any{"x": 1}, "message": "fine"})
	case "huge":
		chunk := strings.Repeat("x", 64<<10)
		for i := 0; i < 64; i++ {
			if _, err := os.Stdout.WriteString(chunk); err != nil {
				os.Exit(7)
			}
		}
	case "hugeerr":
		chunk := strings.Repeat("e", 64<<10)
		for i := 0; i < 32; i++ {
			os.Stderr.WriteString(chunk)
		}
		respond(map[string]any{"protocol_version": 1, "status": "delivered"})
	case "exit3":
		respond(map[string]any{"protocol_version": 1, "status": "delivered"})
		os.Exit(3)
	case "crash":
		panic("plugin crashed")
	case "sleep":
		time.Sleep(time.Hour)
	case "fork":
		self, _ := os.Executable()
		cmd := exec.Command(self, "child", s["pidfile"])
		_ = cmd.Start()
		for i := 0; i < 500; i++ {
			if _, err := os.Stat(s["pidfile"]); err == nil {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		time.Sleep(time.Hour)
	default:
		fmt.Fprintln(os.Stderr, "unknown mode", s["mode"])
		os.Exit(5)
	}
}
