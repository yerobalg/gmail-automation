package main

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"encoding/base64"
	"flag"
	"fmt"
	"mime"
	"net/mail"
	"net/smtp"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type fileInfo struct {
	path string
	name string
	size int64
}

type config struct {
	user       string
	pwd        string
	senderName string
	subject    string
	root       string
	smtpHost   string
	smtpPort   string
	maxN       int
	maxB       int64
}

func main() {
	envPath := flag.String("env", "", "path to .env file (optional)")
	flag.Parse()

	if err := loadDotEnv(*envPath); err != nil {
		fmt.Fprintln(os.Stderr, "warning:", err)
	}

	cfg, err := loadConfig()
	if err != nil {
		die(err)
	}
	if err := run(cfg); err != nil {
		die(err)
	}
}

func die(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}

func loadDotEnv(explicit string) error {
	var candidates []string
	if explicit != "" {
		candidates = append(candidates, explicit)
	}
	if exe, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Join(filepath.Dir(exe), ".env"))
	}
	candidates = append(candidates, ".env")

	for _, p := range candidates {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		parseEnv(string(data))
		return nil
	}
	return fmt.Errorf(".env not found (looked in %v)", candidates)
}

func parseEnv(s string) {
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		i := strings.IndexByte(line, '=')
		if i < 0 {
			continue
		}
		k := strings.TrimSpace(line[:i])
		v := strings.TrimSpace(line[i+1:])
		if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[0] == v[len(v)-1] {
			v = v[1 : len(v)-1]
		}
		if _, set := os.LookupEnv(k); !set {
			os.Setenv(k, v)
		}
	}
}

func loadConfig() (*config, error) {
	c := &config{
		user:       os.Getenv("SMTP_USER"),
		pwd:        os.Getenv("SMTP_PASSWORD"),
		senderName: getDefault("SENDER_NAME", "Laksono"),
		subject:    getDefault("EMAIL_SUBJECT", "Invoice"),
		root:       getDefault("ATTACHMENTS_ROOT", "./attachments"),
		smtpHost:   getDefault("SMTP_HOST", "smtp.gmail.com"),
		smtpPort:   getDefault("SMTP_PORT", "465"),
	}
	if c.user == "" || c.pwd == "" {
		return nil, fmt.Errorf("SMTP_USER and SMTP_PASSWORD are required in .env")
	}
	if _, err := strconv.Atoi(c.smtpPort); err != nil {
		return nil, fmt.Errorf("SMTP_PORT must be numeric, got %q", c.smtpPort)
	}
	n, err := strconv.Atoi(getDefault("MAX_ATTACHMENTS_PER_EMAIL", "5"))
	if err != nil || n <= 0 {
		return nil, fmt.Errorf("MAX_ATTACHMENTS_PER_EMAIL must be a positive integer")
	}
	c.maxN = n

	mb, err := strconv.ParseInt(getDefault("MAX_SIZE_MB", "15"), 10, 64)
	if err != nil || mb <= 0 {
		return nil, fmt.Errorf("MAX_SIZE_MB must be a positive integer")
	}
	c.maxB = mb * 1024 * 1024

	return c, nil
}

func getDefault(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

func dialSMTP(host, port string) (*smtp.Client, error) {
	addr := host + ":" + port
	tlsCfg := &tls.Config{ServerName: host}

	if port == "465" {
		conn, err := tls.Dial("tcp", addr, tlsCfg)
		if err != nil {
			return nil, fmt.Errorf("tls dial %s: %w", addr, err)
		}
		client, err := smtp.NewClient(conn, host)
		if err != nil {
			conn.Close()
			return nil, fmt.Errorf("smtp client: %w", err)
		}
		return client, nil
	}

	client, err := smtp.Dial(addr)
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", addr, err)
	}
	if ok, _ := client.Extension("STARTTLS"); !ok {
		client.Quit()
		return nil, fmt.Errorf("server %s does not advertise STARTTLS on port %s", host, port)
	}
	if err := client.StartTLS(tlsCfg); err != nil {
		client.Quit()
		return nil, fmt.Errorf("starttls: %w", err)
	}
	return client, nil
}

func run(c *config) error {
	client, err := dialSMTP(c.smtpHost, c.smtpPort)
	if err != nil {
		return err
	}
	defer client.Quit()

	if err := client.Auth(smtp.PlainAuth("", c.user, c.pwd, c.smtpHost)); err != nil {
		return fmt.Errorf("auth: %w", err)
	}

	entries, err := os.ReadDir(c.root)
	if err != nil {
		return fmt.Errorf("read attachments root: %w", err)
	}
	var recipients []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if _, err := mail.ParseAddress(name); err != nil {
			return fmt.Errorf("folder %q is not a valid email address", name)
		}
		recipients = append(recipients, name)
	}
	if len(recipients) == 0 {
		return fmt.Errorf("no valid recipient folders found in %s", c.root)
	}

	reader := bufio.NewReader(os.Stdin)
	sent, skipped, failed := 0, 0, 0

	for _, rcpt := range recipients {
		folder := filepath.Join(c.root, rcpt)
		files, err := listPDFs(folder)
		if err != nil {
			fmt.Printf("[skip] %s: %v\n", rcpt, err)
			continue
		}
		if len(files) == 0 {
			fmt.Printf("[skip] no PDFs in %s\n", folder)
			continue
		}

		chunks, err := chunkFiles(files, c.maxN, c.maxB)
		if err != nil {
			return err
		}

		fmt.Printf("\n[%s] %d email(s) to send\n", rcpt, len(chunks))
		for idx, chunk := range chunks {
			body := buildBody(names(chunk), c.senderName)
			fmt.Printf("\n--- email %d of %d ---\n", idx+1, len(chunks))
			preview(rcpt, c.subject, body, chunk)
		}
		fmt.Printf("Send all %d email(s) to %s? [y/N]: ", len(chunks), rcpt)
		line, _ := reader.ReadString('\n')
		ans := strings.ToLower(strings.TrimSpace(line))
		if ans != "y" && ans != "yes" {
			skipped += len(chunks)
			fmt.Println("Skipped.")
			continue
		}

		for idx, chunk := range chunks {
			body := buildBody(names(chunk), c.senderName)
			raw, err := buildMessage(c.user, c.senderName, rcpt, c.subject, body, chunk)
			if err != nil {
				failed++
				fmt.Printf("email #%d build failed: %v\n", idx+1, err)
				continue
			}
			if err := sendOne(client, c.user, rcpt, raw); err != nil {
				failed++
				fmt.Printf("email #%d send failed: %v\n", idx+1, err)
				continue
			}
			sent++
			fmt.Printf("email #%d sent\n", idx+1)
		}
	}

	fmt.Printf("\nDone. Sent: %d, Skipped: %d, Failed: %d\n", sent, skipped, failed)
	return nil
}

func listPDFs(folder string) ([]fileInfo, error) {
	entries, err := os.ReadDir(folder)
	if err != nil {
		return nil, err
	}
	var out []fileInfo
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if !strings.EqualFold(filepath.Ext(e.Name()), ".pdf") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			return nil, err
		}
		out = append(out, fileInfo{
			path: filepath.Join(folder, e.Name()),
			name: e.Name(),
			size: info.Size(),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i].name) < strings.ToLower(out[j].name)
	})
	return out, nil
}

func chunkFiles(files []fileInfo, maxN int, maxB int64) ([][]fileInfo, error) {
	var chunks [][]fileInfo
	var cur []fileInfo
	var size int64
	for _, f := range files {
		if f.size > maxB {
			return nil, fmt.Errorf("file %s (%d bytes) exceeds per-email limit %d bytes", f.name, f.size, maxB)
		}
		if len(cur) >= maxN || size+f.size > maxB {
			chunks = append(chunks, cur)
			cur, size = nil, 0
		}
		cur = append(cur, f)
		size += f.size
	}
	if len(cur) > 0 {
		chunks = append(chunks, cur)
	}
	return chunks, nil
}

func names(fs []fileInfo) []string {
	out := make([]string, len(fs))
	for i, f := range fs {
		out[i] = f.name
	}
	return out
}

func buildBody(names []string, sender string) string {
	var b strings.Builder
	b.WriteString("Berikut kami kirimkan Invoice dengan rincian sebagai berikut\n")
	for i, n := range names {
		fmt.Fprintf(&b, "    %d. %s\n", i+1, n)
	}
	b.WriteString("\nAtas perhatiannya, kami ucapkan terima kasih.\n\n")
	b.WriteString("Best Regards,\n\n")
	b.WriteString(sender)
	b.WriteString("\n")
	return b.String()
}

func preview(to, subject, body string, files []fileInfo) {
	var total int64
	for _, f := range files {
		total += f.size
	}
	fmt.Println(strings.Repeat("=", 60))
	fmt.Printf("To: %s\nSubject: %s\n", to, subject)
	fmt.Printf("Attachments (%d, %.2f MB):\n", len(files), float64(total)/1024/1024)
	for _, f := range files {
		fmt.Printf("  - %s  (%.2f MB)\n", f.name, float64(f.size)/1024/1024)
	}
	fmt.Println(strings.Repeat("-", 60))
	fmt.Println(body)
	fmt.Println(strings.Repeat("=", 60))
}

func buildMessage(user, senderName, to, subject, body string, files []fileInfo) ([]byte, error) {
	boundary := fmt.Sprintf("----=_GmailAutoBoundary_%d", os.Getpid())
	var buf bytes.Buffer

	fmt.Fprintf(&buf, "From: %s <%s>\r\n", mime.QEncoding.Encode("utf-8", senderName), user)
	fmt.Fprintf(&buf, "To: %s\r\n", to)
	fmt.Fprintf(&buf, "Subject: %s\r\n", mime.QEncoding.Encode("utf-8", subject))
	buf.WriteString("MIME-Version: 1.0\r\n")
	fmt.Fprintf(&buf, "Content-Type: multipart/mixed; boundary=\"%s\"\r\n\r\n", boundary)

	// text body part
	fmt.Fprintf(&buf, "--%s\r\n", boundary)
	buf.WriteString("Content-Type: text/plain; charset=\"utf-8\"\r\n")
	buf.WriteString("Content-Transfer-Encoding: 8bit\r\n\r\n")
	buf.WriteString(body)
	buf.WriteString("\r\n")

	// attachment parts
	for _, f := range files {
		data, err := os.ReadFile(f.path)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", f.path, err)
		}
		fmt.Fprintf(&buf, "--%s\r\n", boundary)
		fmt.Fprintf(&buf, "Content-Type: application/pdf; name=\"%s\"\r\n", f.name)
		fmt.Fprintf(&buf, "Content-Disposition: attachment; filename=\"%s\"\r\n", f.name)
		buf.WriteString("Content-Transfer-Encoding: base64\r\n\r\n")
		encoded := base64.StdEncoding.EncodeToString(data)
		for i := 0; i < len(encoded); i += 76 {
			end := i + 76
			if end > len(encoded) {
				end = len(encoded)
			}
			buf.WriteString(encoded[i:end])
			buf.WriteString("\r\n")
		}
	}

	fmt.Fprintf(&buf, "--%s--\r\n", boundary)
	return buf.Bytes(), nil
}

func sendOne(c *smtp.Client, from, to string, msg []byte) error {
	if err := c.Reset(); err != nil {
		return fmt.Errorf("reset: %w", err)
	}
	if err := c.Mail(from); err != nil {
		return fmt.Errorf("mail: %w", err)
	}
	if err := c.Rcpt(to); err != nil {
		return fmt.Errorf("rcpt: %w", err)
	}
	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("data: %w", err)
	}
	if _, err := w.Write(msg); err != nil {
		w.Close()
		return fmt.Errorf("write: %w", err)
	}
	return w.Close()
}
