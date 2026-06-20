package notify

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/mail"
	"net/smtp"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"fnmonitor/internal/config"
)

type Message struct {
	Title    string            `json:"title"`
	Body     string            `json:"body"`
	Severity string            `json:"severity"`
	Labels   map[string]string `json:"labels,omitempty"`
	Time     time.Time         `json:"time"`
}

type Result struct {
	Channel string `json:"channel"`
	OK      bool   `json:"ok"`
	Error   string `json:"error,omitempty"`
}

type Notifier interface {
	Name() string
	Send(ctx context.Context, msg Message) error
}

type Manager struct {
	enabled   bool
	notifiers []Notifier
}

func New(cfg config.NotifyRootConfig) *Manager {
	m := &Manager{enabled: cfg.Enabled}
	if !cfg.Enabled {
		return m
	}
	if cfg.Channels.Stdout.Enabled {
		m.notifiers = append(m.notifiers, stdoutNotifier{})
	}
	if cfg.Channels.FNOSEmail.Enabled {
		m.notifiers = append(m.notifiers, fnosEmailNotifier{cfg: cfg.Channels.FNOSEmail})
	}
	if cfg.Channels.SMTP.Enabled {
		m.notifiers = append(m.notifiers, smtpNotifier{cfg: cfg.Channels.SMTP})
	}
	if cfg.Channels.Webhook.Enabled {
		m.notifiers = append(m.notifiers, webhookNotifier{cfg: cfg.Channels.Webhook})
	}
	return m
}

func (m *Manager) Enabled() bool {
	return m.enabled && len(m.notifiers) > 0
}

func (m *Manager) Send(ctx context.Context, msg Message) []Result {
	if msg.Time.IsZero() {
		msg.Time = time.Now()
	}
	if !m.Enabled() {
		return []Result{{Channel: "none", OK: false, Error: "notification disabled or no channel enabled"}}
	}
	results := make([]Result, 0, len(m.notifiers))
	for _, n := range m.notifiers {
		err := n.Send(ctx, msg)
		res := Result{Channel: n.Name(), OK: err == nil}
		if err != nil {
			res.Error = err.Error()
		}
		results = append(results, res)
	}
	return results
}

type stdoutNotifier struct{}

func (stdoutNotifier) Name() string { return "stdout" }

func (stdoutNotifier) Send(_ context.Context, msg Message) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(os.Stdout, string(data))
	return err
}

type fnosEmailNotifier struct {
	cfg config.FNOSEmailNotifyConfig
}

func (n fnosEmailNotifier) Name() string { return "fnos_email" }

func (n fnosEmailNotifier) Send(ctx context.Context, msg Message) error {
	switch n.cfg.Mode {
	case "command":
		return n.sendCommand(ctx, msg)
	case "http":
		return n.sendHTTP(ctx, msg)
	default:
		return fmt.Errorf("unsupported fnos_email mode %q", n.cfg.Mode)
	}
}

func (n fnosEmailNotifier) sendCommand(ctx context.Context, msg Message) error {
	timeout := n.cfg.Timeout.Duration
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	args := make([]string, 0, len(n.cfg.Args))
	for _, arg := range n.cfg.Args {
		args = append(args, renderToken(arg, msg))
	}
	cmd := exec.CommandContext(cctx, n.cfg.Command, args...)
	cmd.Env = append(os.Environ(),
		"FNMONITOR_TITLE="+msg.Title,
		"FNMONITOR_BODY="+msg.Body,
		"FNMONITOR_SEVERITY="+msg.Severity,
		"FNMONITOR_TIME="+msg.Time.Format(time.RFC3339),
	)
	if n.cfg.BodyStdin {
		cmd.Stdin = strings.NewReader(msg.Body)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return fmt.Errorf("%w: %s", err, msg)
		}
		return err
	}
	return nil
}

func (n fnosEmailNotifier) sendHTTP(ctx context.Context, msg Message) error {
	timeout := n.cfg.Timeout.Duration
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	payload, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	method := n.cfg.HTTPMethod
	if method == "" {
		method = http.MethodPost
	}
	req, err := http.NewRequestWithContext(cctx, method, n.cfg.HTTPURL, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range n.cfg.HTTPHeaders {
		req.Header.Set(k, renderToken(v, msg))
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("fnos email http status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

type smtpNotifier struct {
	cfg config.SMTPNotifyConfig
}

func (n smtpNotifier) Name() string { return "smtp" }

func (n smtpNotifier) Send(ctx context.Context, msg Message) error {
	password := n.cfg.Password
	if password == "" && n.cfg.PasswordEnv != "" {
		password = os.Getenv(n.cfg.PasswordEnv)
	}
	addr := net.JoinHostPort(n.cfg.Host, strconv.Itoa(n.cfg.Port))
	conn, err := dialSMTP(ctx, addr, n.cfg.Host, n.cfg.TLS, n.cfg.InsecureSkipVerify)
	if err != nil {
		return err
	}
	client, err := smtp.NewClient(conn, n.cfg.Host)
	if err != nil {
		_ = conn.Close()
		return err
	}
	defer client.Close()

	if n.cfg.StartTLS && !n.cfg.TLS {
		tlsCfg := &tls.Config{ServerName: n.cfg.Host, InsecureSkipVerify: n.cfg.InsecureSkipVerify}
		if ok, _ := client.Extension("STARTTLS"); ok {
			if err := client.StartTLS(tlsCfg); err != nil {
				return err
			}
		}
	}
	if n.cfg.Username != "" || password != "" {
		auth := smtp.PlainAuth("", n.cfg.Username, password, n.cfg.Host)
		if err := client.Auth(auth); err != nil {
			return err
		}
	}
	if err := client.Mail(n.cfg.From); err != nil {
		return err
	}
	for _, to := range n.cfg.To {
		if err := client.Rcpt(to); err != nil {
			return err
		}
	}
	w, err := client.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write([]byte(smtpMessage(n.cfg.From, n.cfg.FromName, n.cfg.To, msg))); err != nil {
		_ = w.Close()
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return client.Quit()
}

func dialSMTP(ctx context.Context, addr, host string, useTLS, insecure bool) (net.Conn, error) {
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	if useTLS {
		return tls.DialWithDialer(dialer, "tcp", addr, &tls.Config{ServerName: host, InsecureSkipVerify: insecure})
	}
	return dialer.DialContext(ctx, "tcp", addr)
}

func smtpMessage(from, fromName string, to []string, msg Message) string {
	subject := mime.QEncoding.Encode("utf-8", msg.Title)
	fromHeader := from
	if strings.TrimSpace(fromName) != "" {
		fromHeader = (&mail.Address{Name: fromName, Address: from}).String()
	}
	body := msg.Body
	headers := []string{
		"From: " + fromHeader,
		"To: " + strings.Join(to, ", "),
		"Subject: " + subject,
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=UTF-8",
		"Content-Transfer-Encoding: 8bit",
	}
	return strings.Join(headers, "\r\n") + "\r\n\r\n" + body + "\r\n"
}

type webhookNotifier struct {
	cfg config.WebhookNotifyConfig
}

func (n webhookNotifier) Name() string { return "webhook" }

func (n webhookNotifier) Send(ctx context.Context, msg Message) error {
	timeout := n.cfg.Timeout.Duration
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	payload, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	method := n.cfg.Method
	if method == "" {
		method = http.MethodPost
	}
	req, err := http.NewRequestWithContext(cctx, method, n.cfg.URL, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range n.cfg.Headers {
		req.Header.Set(k, renderToken(v, msg))
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("webhook status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

func renderToken(value string, msg Message) string {
	replacer := strings.NewReplacer(
		"{{title}}", msg.Title,
		"{{body}}", msg.Body,
		"{{severity}}", msg.Severity,
		"{{time}}", msg.Time.Format(time.RFC3339),
	)
	return replacer.Replace(value)
}
