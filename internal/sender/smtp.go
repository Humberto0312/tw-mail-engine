package sender

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/smtp"
	"sort"
	"strings"
	"time"
)

// Mailer entrega correos al MX del destinatario por el puerto 25.
// sourceIPs prepara el camino al pool de IPs (IP dedicada por cliente):
// si hay una IP, se usa como dirección de origen del socket.
type Mailer struct {
	hostname  string
	sourceIPs []string
}

func NewMailer(hostname string, sourceIPs []string) *Mailer {
	return &Mailer{hostname: hostname, sourceIPs: sourceIPs}
}

// Deliver resuelve el MX del destinatario y entrega el mensaje (ya firmado).
func (m *Mailer) Deliver(ctx context.Context, fromAddr, toAddr string, raw []byte) error {
	domain := addrDomain(toAddr)
	if domain == "" {
		return fmt.Errorf("destinatario inválido: %s", toAddr)
	}
	hosts, err := lookupMX(domain)
	if err != nil || len(hosts) == 0 {
		return fmt.Errorf("sin MX para %s: %v", domain, err)
	}

	var lastErr error
	for _, mx := range hosts {
		if err := m.deliverTo(ctx, mx, fromAddr, toAddr, raw); err != nil {
			lastErr = err
			continue // probar el siguiente MX
		}
		return nil // entregado
	}
	return fmt.Errorf("no se pudo entregar a %s: %w", domain, lastErr)
}

func (m *Mailer) deliverTo(ctx context.Context, mxHost, fromAddr, toAddr string, raw []byte) error {
	dialer := &net.Dialer{Timeout: 20 * time.Second}
	if len(m.sourceIPs) > 0 && m.sourceIPs[0] != "" {
		if ip := net.ParseIP(m.sourceIPs[0]); ip != nil {
			dialer.LocalAddr = &net.TCPAddr{IP: ip}
		}
	}
	// tcp4: la IPv6 de OVH no enruta; forzamos IPv4 para la entrega.
	conn, err := dialer.DialContext(ctx, "tcp4", net.JoinHostPort(mxHost, "25"))
	if err != nil {
		return err
	}
	c, err := smtp.NewClient(conn, mxHost)
	if err != nil {
		conn.Close()
		return err
	}
	defer c.Close()

	if err := c.Hello(m.hostname); err != nil {
		return err
	}
	if ok, _ := c.Extension("STARTTLS"); ok {
		// TLS oportunista: ciframos si el MX lo soporta, sin exigir cert válido.
		if err := c.StartTLS(&tls.Config{ServerName: mxHost, InsecureSkipVerify: true}); err != nil {
			return err
		}
	}
	if err := c.Mail(fromAddr); err != nil {
		return err
	}
	if err := c.Rcpt(toAddr); err != nil {
		return err
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(raw); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return c.Quit()
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
