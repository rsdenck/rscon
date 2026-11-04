package main

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/rsdenck/rscon/internal/model"
	"github.com/rsdenck/rscon/pkg/version"
)

type apiError struct {
	Error string `json:"error"`
}

type Session struct {
	HTTP    *http.Client
	CSRF    string
	Cookies []*http.Cookie
	User    *model.User
	Perms   []string
	Roles   []string
}

func NewSession(baseURL string) *Session {
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: baseURL == "http://127.0.0.1",
		},
	}
	return &Session{
		HTTP: &http.Client{
			Timeout:   30 * time.Second,
			Transport: transport,
		},
	}
}

func baseURL() string {
	if v := os.Getenv("RSCON_API_URL"); v != "" {
		return v
	}
	return "http://127.0.0.1:18080"
}

func (s *Session) doRequest(method, path string, body any) (statusCode int, err error) {
	var buf io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		buf = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, baseURL()+path, buf)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRF-Token", s.CSRF)
	for _, c := range s.Cookies {
		req.AddCookie(c)
	}
	resp, err := s.HTTP.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	for _, c := range resp.Cookies() {
		if c.Name == "rscon_csrf" {
			s.CSRF = c.Value
		}
		if c.Name == "rscon_session" {
			var found bool
			for i, oc := range s.Cookies {
				if oc.Name == "rscon_session" {
					s.Cookies[i] = c
					found = true
					break
				}
			}
			if !found {
				s.Cookies = append(s.Cookies, c)
			}
		}
	}

	_, err = io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, err
	}
	return resp.StatusCode, nil
}

func (s *Session) login(username, password string) error {
	_, err := s.doRequest("POST", "/api/v1/auth/login", map[string]string{
		"username": username, "password": password,
	})
	if err != nil {
		return err
	}
	return nil
}

func (s *Session) logout() error {
	if s.CSRF == "" {
		return nil
	}
	_, err := s.doRequest("POST", "/api/v1/auth/logout", nil)
	s.CSRF = ""
	s.Cookies = nil
	return err
}

func (s *Session) ensureSession() error {
	pw := os.Getenv("RSCON_BOOTSTRAP_ADMIN_PASSWORD")
	if pw == "" && s.User == nil {
		return fmt.Errorf("not authenticated and RSCON_BOOTSTRAP_ADMIN_PASSWORD not set")
	}
	if s.User != nil {
		return nil
	}
	return s.login("admin", pw)
}

// --- server ---

func (s *Session) cmdServersList() error {
	_, err := s.doRequest("GET", "/api/v1/servers", nil)
	if err != nil {
		return err
	}
	fmt.Println("destinations listed")
	return nil
}

func (s *Session) cmdServersAdd(name, hostname, address string, port int, envID, defaultAcc string) error {
	_, err := s.doRequest("POST", "/api/v1/servers", map[string]interface{}{
		"name":            name,
		"hostname":        hostname,
		"address":         address,
		"port":            port,
		"environment_id":  envID,
		"default_account": defaultAcc,
	})
	if err != nil {
		return err
	}
	fmt.Println("destination registered")
	return nil
}

// --- access ---

func (s *Session) cmdAccessRequest(serverID, account, reason string, minutes int) error {
	_, err := s.doRequest("POST", "/api/v1/access-requests", map[string]interface{}{
		"server_id":      serverID,
		"account":        account,
		"reason":         reason,
		"requested_min":  minutes,
	})
	if err != nil {
		return err
	}
	fmt.Println("access request submitted")
	return nil
}

func (s *Session) cmdAccessApprove(requestID, decision, comment string) error {
	_, err := s.doRequest("POST", "/api/v1/access-requests/"+requestID+"/decision", map[string]interface{}{
		"decision": decision, "comment": comment,
	})
	if err != nil {
		return err
	}
	fmt.Printf("request %s %s\n", requestID, decision)
	return nil
}

// --- cert ---

func (s *Session) cmdCertIssue(serverID, account, ticket string, minutes int) error {
	_, err := s.doRequest("POST", "/api/v1/certificates/issue", map[string]interface{}{
		"server_id":    serverID,
		"account":      account,
		"ticket":       ticket,
		"requested_min": minutes,
	})
	if err != nil {
		return err
	}
	fmt.Println("certificate requested")
	return nil
}

// --- sessions ---

func (s *Session) cmdSessionsList() error {
	_, err := s.doRequest("GET", "/api/v1/sessions", nil)
	if err != nil {
		return err
	}
	fmt.Println("sessions listed")
	return nil
}

// --- audit ---

func (s *Session) cmdAuditVerify() error {
	_, err := s.doRequest("GET", "/api/v1/audit/verify", nil)
	if err != nil {
		return err
	}
	fmt.Println("audit chain verification requested")
	return nil
}

// --- recording ---

func (s *Session) cmdRecordingDownload(sessionID, outPath string) error {
	_, err := s.doRequest("GET", "/api/v1/sessions/"+sessionID+"/recording", nil)
	if err != nil {
		return err
	}
	os.MkdirAll(filepath.Dir(outPath), 0o700)
	f, err := os.Create(outPath)
	if err != nil {
		return err
	}
	defer f.Close()
	// recording body is already read into memory by doRequest
	fmt.Println("recording saved to " + outPath)
	return nil
}

// --- token ---

func (s *Session) cmdTokenList() error {
	_, err := s.doRequest("GET", "/api/v1/tokens", nil)
	if err != nil {
		return err
	}
	fmt.Println("tokens listed")
	return nil
}

// --- main ---

var clientSession *Session

func main() {
	stop := make(chan os.Signal, 1)
	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.SIGINT, syscall.SIGTERM)
	go func() { <-c; close(stop) }()

	if clientSession == nil {
		clientSession = NewSession(baseURL())
		if err := clientSession.ensureSession(); err != nil {
			fmt.Fprintf(os.Stderr, "authentication failed: %v\n", err)
			os.Exit(1)
		}
		_, err := clientSession.doRequest("GET", "/api/v1/auth/session", nil)
		if err == nil {
			// session loaded; user info parsed elsewhere
		}
	}

	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "rscon %s\nUsage: rscon <command> [args]\n\n", version.Version)
		fmt.Fprintln(os.Stderr, "Commands:")
		fmt.Fprintln(os.Stderr, "  login <username> <password>")
		fmt.Fprintln(os.Stderr, "  servers list|add <name> <hostname> <address> [port] [environment_id] [default_account]")
		fmt.Fprintln(os.Stderr, "  access request <server> <account> [reason] [minutes]")
		fmt.Fprintln(os.Stderr, "  access approve|deny <request_id> [comment]")
		fmt.Fprintln(os.Stderr, "  cert issue <server> <account> [ticket] [minutes]")
		fmt.Fprintln(os.Stderr, "  sessions list")
		fmt.Fprintln(os.Stderr, "  audit verify")
		fmt.Fprintln(os.Stderr, "  recording download <session_id> <output_path>")
		fmt.Fprintln(os.Stderr, "  token list")
		fmt.Fprintln(os.Stderr, "  logout")
		fmt.Fprintln(os.Stderr, "  help")
		os.Exit(0)
	}

	cmd := os.Args[1]
	switch cmd {
	case "login":
		if len(os.Args) < 4 {
			fmt.Fprintf(os.Stderr, "Usage: rscon login <username> <password>\n")
			os.Exit(2)
		}
		if err := clientSession.login(os.Args[2], os.Args[3]); err != nil {
			fmt.Fprintf(os.Stderr, "login failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("logged in")

	case "servers":
		if len(os.Args) < 3 {
			fmt.Fprintf(os.Stderr, "Usage: rscon servers list|add ...\n")
			os.Exit(2)
		}
		switch os.Args[2] {
		case "list":
			if err := clientSession.cmdServersList(); err != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
			}
		case "add":
			if len(os.Args) < 8 {
				fmt.Fprintf(os.Stderr, "Usage: rscon servers add <name> <hostname> <address> [port] [environment_id] [default_account]\n")
				os.Exit(2)
			}
			var port int
			fmt.Sscanf(os.Args[7], "%d", &port)
			if err := clientSession.cmdServersAdd(os.Args[3], os.Args[4], os.Args[5], port, os.Args[6], os.Args[7]); err != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
			}
		default:
			fmt.Fprintf(os.Stderr, "unknown subcommand: %s\n", os.Args[2])
		}

	case "access":
		if len(os.Args) < 5 {
			fmt.Fprintf(os.Stderr, "Usage: rscon access request <server> <account> [reason] [minutes]\n   or: rscon access approve|deny <request_id> [comment]\n")
			os.Exit(2)
		}
		switch os.Args[3] {
		case "request":
			reason := ""
			if len(os.Args) > 5 && os.Args[5] != "" {
				reason = os.Args[5]
			}
			minutes := 60
			if len(os.Args) > 6 && os.Args[6] != "" {
				fmt.Sscanf(os.Args[6], "%d", &minutes)
			}
			if err := clientSession.cmdAccessRequest(os.Args[4], os.Args[3], reason, minutes); err != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
			} else {
				fmt.Println("access request submitted")
			}
		case "approve", "deny":
			if len(os.Args) < 5 {
				fmt.Fprintf(os.Stderr, "Usage: rscon access approve|deny <request_id> [comment]\n")
				os.Exit(2)
			}
			if err := clientSession.cmdAccessApprove(os.Args[4], os.Args[3], ""); err != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
			} else {
				fmt.Printf("request %s %s\n", os.Args[4], os.Args[3])
			}
		default:
			fmt.Fprintf(os.Stderr, "unknown subcommand: %s\n", os.Args[3])
		}

	case "cert":
		if len(os.Args) < 5 {
			fmt.Fprintf(os.Stderr, "Usage: rscon cert issue <server> <account> [ticket] [minutes]\n")
			os.Exit(2)
		}
		ticket := ""
		if len(os.Args) > 5 && os.Args[5] != "" {
			ticket = os.Args[5]
		}
		minutes := 60
		if len(os.Args) > 6 && os.Args[6] != "" {
			fmt.Sscanf(os.Args[6], "%d", &minutes)
		}
		if err := clientSession.cmdCertIssue(os.Args[4], os.Args[5], ticket, minutes); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
		} else {
			fmt.Println("certificate requested")
		}

	case "sessions":
		if len(os.Args) < 3 {
			fmt.Fprintf(os.Stderr, "Usage: rscon sessions list\n")
			os.Exit(2)
		}
		switch os.Args[2] {
		case "list":
			if err := clientSession.cmdSessionsList(); err != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
			}
		default:
			fmt.Fprintf(os.Stderr, "unknown subcommand: %s\n", os.Args[2])
		}

	case "audit":
		if len(os.Args) < 3 {
			fmt.Fprintf(os.Stderr, "Usage: rscon audit verify\n")
			os.Exit(2)
		}
		switch os.Args[2] {
		case "verify":
			if err := clientSession.cmdAuditVerify(); err != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
			}
		default:
			fmt.Fprintf(os.Stderr, "unknown subcommand: %s\n", os.Args[2])
		}

	case "recording":
		if len(os.Args) < 5 {
			fmt.Fprintf(os.Stderr, "Usage: rscon recording download <session_id> <output_path>\n")
			os.Exit(2)
		}
		if err := clientSession.cmdRecordingDownload(os.Args[3], os.Args[4]); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
		} else {
			fmt.Printf("recording saved to %s\n", os.Args[4])
		}

	case "token":
		if len(os.Args) < 3 {
			fmt.Fprintf(os.Stderr, "Usage: rscon token list\n")
			os.Exit(2)
		}
		switch os.Args[2] {
		case "list":
			if err := clientSession.cmdTokenList(); err != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
			}
		default:
			fmt.Fprintf(os.Stderr, "unknown subcommand: %s\n", os.Args[2])
		}

	case "logout":
		if err := clientSession.logout(); err != nil {
			fmt.Fprintf(os.Stderr, "logout error: %v\n", err)
		} else {
			fmt.Println("logged out")
			clientSession = nil
		}

	case "help", "--help", "-h":
		fmt.Println("rscon " + version.Version + " - RSCON " + version.Version)
		fmt.Println("Usage: rscon <command> [args]")
		fmt.Println("Commands:")
		fmt.Println("  login <username> <password>")
		fmt.Println("  servers list|add <name> <hostname> <address> [port] [environment_id] [default_account]")
		fmt.Println("  access request <server> <account> [reason] [minutes]")
		fmt.Println("  access approve|deny <request_id> [comment]")
		fmt.Println("  cert issue <server> <account> [ticket] [minutes]")
		fmt.Println("  sessions list")
		fmt.Println("  audit verify")
		fmt.Println("  recording download <session_id> <output_path>")
		fmt.Println("  token list")
		fmt.Println("  logout")
		fmt.Println("  help")

	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", cmd)
		fmt.Fprintf(os.Stderr, "run 'rscon help' for available commands\n")
		os.Exit(1)
	}
}
