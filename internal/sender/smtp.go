package sender

import (
	"context"
	"crypto/tls"
	"net"
	"net/smtp"
	"net/textproto"
	"sort"
	"strings"
	"time"
)

// DeliverError — error de entrega clasificado. Permanent=true (5xx / dominio
// inválido) → no reintentar y suprimir. Permanent=false (4xx / greylist / red)
// → reintentar más tarde.
type DeliverError struct {
	Permanent bool
	Msg       string
}

func (e *DeliverError) Error() string { return e.Msg }

// Mailer entrega correos al MX del destinatario por el puerto 25.
type Mailer struct {
	hostname  string
	sourceIPs []string
}

func NewMailer(hostname string, sourceIPs []string) *Mailer {
	return &Mailer{hostname: hostname, sourceIPs: sourceIPs}
}

// Deliver resuelve el MX del destinatario y entrega el mensaje (ya firmado).
// Devuelve nil si entregó, o *DeliverError clasificado.
func (m *Mailer) Deliver(ctx context.Context, fromAddr, toAddr string, raw []byte) *DeliverError {
	domain := addrDomain(toAddr)
	if domain == "" {
		return &DeliverError{Permanent: true, Msg: "destinatario inválido: " + toAddr}
	}
	hosts, err := lookupMX(domain)
	if err != nil || len(hosts) == 0 {
		return &DeliverError{Permanent: true, Msg: "sin MX para " + domain}
	}

	var last *DeliverError
	for _, mx := range hosts {
		if de := m.deliverTo(ctx, mx, fromAddr, toAddr, raw); de != nil {
			last = de
			continue
		}
		return nil
	}
	return last
}

func (m *Mailer) deliverTo(ctx context.Context, mxHost, fromAddr, toAddr string, raw []byte) *DeliverError {
	dialer := &net.Dialer{Timeout: 20 * time.Second}
	if len(m.sourceIPs) > 0 && m.sourceIPs[0] != "" {
		if ip := net.ParseIP(m.sourceIPs[0]); ip != nil {
			dialer.LocalAddr = &net.TCPAddr{IP: ip}
		}
	}
	conn, err := dialer.DialContext(ctx, "tcp4", net.JoinHostPort(mxHost, "25"))
	if err != nil {
		return &DeliverError{Permanent: false, Msg: err.Error()}
	}
	c, err := smtp.NewClient(conn, mxHost)
	if err != nil {
		conn.Close()
		return &DeliverError{Permanent: false, Msg: err.Error()}
	}
	defer c.Close()

	if err := c.Hello(m.hostname); err != nil {
		return classify(err)
	}
	if ok, _ := c.Extension("STARTTLS"); ok {
		if err := c.StartTLS(&tls.Config{ServerName: mxHost, InsecureSkipVerify: true}); err != nil {
			return &DeliverError{Permanent: false, Msg: err.Error()}
		}
	}
	if err := c.Mail(fromAddr); err != nil {
		return classify(err)
	}
	if err := c.Rcpt(toAddr); err != nil {
		return classify(err)
	}
	w, err := c.Data()
	if err != nil {
		return classify(err)
	}
	if _, err := w.Write(raw); err != nil {
		return &DeliverError{Permanent: false, Msg: err.Error()}
	}
	if err := w.Close(); err != nil {
		return classify(err)
	}
	if err := c.Quit(); err != nil {
		// ya entregó; un error en QUIT no invalida la entrega
		return nil
	}
	return nil
}

// classify — 5xx = permanente; 4xx u otros = temporal (reintentar).
func classify(err error) *DeliverError {
	if err == nil {
		return nil
	}
	if te, ok := err.(*textproto.Error); ok {
		return &DeliverError{Permanent: te.Code >= 500, Msg: err.Error()}
	}
	return &DeliverError{Permanent: false, Msg: err.Error()}
}

func lookupMX(domain string) ([]string, error) {
	mxs, err := net.LookupMX(domain)
	if err != nil {
		return nil, err
	}
	sort.Slice(mxs, func(i, j int) bool { return mxs[i].Pref < mxs[j].Pref })
	var hosts []string
	for _, mx := range mxs {
		hosts = append(hosts, strings.TrimSuffix(mx.Host, "."))
	}
	return hosts, nil
}

func addrDomain(addr string) string {
	at := strings.LastIndex(addr, "@")
	if at < 0 || at == len(addr)-1 {
		return ""
	}
	return addr[at+1:]
}
