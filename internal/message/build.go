package message

import (
	"fmt"
	"mime"
	"strings"
	"time"
)

// Outgoing — un correo listo para armar (lo construye api-matrix; el HTML ya
// viene compilado).
type Outgoing struct {
	FromName  string
	FromEmail string
	ReplyTo   string
	To        string
	Subject   string
	HTML      string
	Text      string
	Headers   map[string]string
}

// Build arma el mensaje RFC5322 completo (cabeceras + cuerpo), con CRLF,
// listo para firmar con DKIM y entregar por SMTP.
func Build(o Outgoing, date time.Time, messageID string) []byte {
	var b strings.Builder
	writeHeader(&b, "From", formatAddr(o.FromName, o.FromEmail))
	writeHeader(&b, "To", o.To)
	if o.ReplyTo != "" {
		writeHeader(&b, "Reply-To", o.ReplyTo)
	}
	writeHeader(&b, "Subject", encodeWord(o.Subject))
	writeHeader(&b, "Date", date.Format(time.RFC1123Z))
	writeHeader(&b, "Message-ID", messageID)
	writeHeader(&b, "MIME-Version", "1.0")
	for k, v := range o.Headers {
		writeHeader(&b, k, v)
	}

	switch {
	case o.Text != "" && o.HTML != "":
		boundary := fmt.Sprintf("tw_%d", date.UnixNano())
		writeHeader(&b, "Content-Type", "multipart/alternative; boundary=\""+boundary+"\"")
		b.WriteString("\r\n")
		b.WriteString("--" + boundary + "\r\n")
		b.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
		b.WriteString("Content-Transfer-Encoding: 8bit\r\n\r\n")
		b.WriteString(o.Text + "\r\n\r\n")
		b.WriteString("--" + boundary + "\r\n")
		b.WriteString("Content-Type: text/html; charset=UTF-8\r\n")
		b.WriteString("Content-Transfer-Encoding: 8bit\r\n\r\n")
		b.WriteString(o.HTML + "\r\n\r\n")
		b.WriteString("--" + boundary + "--\r\n")
	default:
		body, ctype := o.HTML, "text/html; charset=UTF-8"
		if body == "" {
			body, ctype = o.Text, "text/plain; charset=UTF-8"
		}
		writeHeader(&b, "Content-Type", ctype)
		writeHeader(&b, "Content-Transfer-Encoding", "8bit")
		b.WriteString("\r\n")
		b.WriteString(body + "\r\n")
	}
	return []byte(b.String())
}

func writeHeader(b *strings.Builder, k, v string) {
	b.WriteString(k)
	b.WriteString(": ")
	b.WriteString(v)
	b.WriteString("\r\n")
}

func formatAddr(name, email string) string {
	if name == "" {
		return email
	}
	return fmt.Sprintf("%s <%s>", encodeWord(name), email)
}

// encodeWord codifica en MIME encoded-word si hay caracteres no ASCII (acentos).
func encodeWord(s string) string {
	if isASCII(s) {
		return s
	}
	return mime.BEncoding.Encode("UTF-8", s)
}

func isASCII(s string) bool {
	for _, r := range s {
		if r > 127 {
			return false
		}
	}
	return true
}
