package domain

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"net"
	"strings"
	"time"

	"tw-mail-engine/internal/store"
)

// Service — alta y verificación de dominios de envío (multi-tenant).
type Service struct {
	st       *store.Store
	selector string
	publicIP string
}

func NewService(st *store.Store, selector, publicIP string) *Service {
	return &Service{st: st, selector: selector, publicIP: publicIP}
}

type Record struct {
	Type  string `json:"type"`
	Name  string `json:"name"`
	Value string `json:"value"`
}

type Records struct {
	DKIM  Record `json:"dkim"`
	SPF   Record `json:"spf"`
	DMARC Record `json:"dmarc"`
}

// Register genera la llave DKIM del dominio, la guarda y devuelve los registros
// DNS que el cliente debe publicar.
func (svc *Service) Register(ctx context.Context, domain, tenantID string) (*Records, error) {
	domain = strings.ToLower(strings.TrimSpace(domain))
	if domain == "" {
		return nil, fmt.Errorf("dominio vacío")
	}
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}
	privPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	})
	pubDER, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		return nil, err
	}
	pubB64 := base64.StdEncoding.EncodeToString(pubDER)

	if err := svc.st.SaveDomain(ctx, store.Domain{
		Domain:      domain,
		TenantID:    tenantID,
		Selector:    svc.selector,
		DKIMPrivate: string(privPEM),
		DKIMPublic:  pubB64,
		Status:      "pending",
		CreatedAt:   time.Now(),
	}); err != nil {
		return nil, err
	}
	return svc.records(domain, pubB64), nil
}

func (svc *Service) records(domain, pubB64 string) *Records {
	return &Records{
		DKIM:  Record{Type: "TXT", Name: svc.selector + "._domainkey." + domain, Value: "v=DKIM1; k=rsa; p=" + pubB64},
		SPF:   Record{Type: "TXT", Name: domain, Value: "v=spf1 ip4:" + svc.publicIP + " -all"},
		DMARC: Record{Type: "TXT", Name: "_dmarc." + domain, Value: "v=DMARC1; p=none;"},
	}
}

// Verify comprueba que DKIM y SPF estén publicados. Si sí, marca verificado.
// Devuelve (ok, problemas, error).
func (svc *Service) Verify(ctx context.Context, domain string) (bool, []string, error) {
	domain = strings.ToLower(strings.TrimSpace(domain))
	d, err := svc.st.GetDomain(ctx, domain)
	if err != nil {
		return false, nil, err
	}
	if d == nil {
		return false, nil, fmt.Errorf("dominio no registrado")
	}

	var problems []string

	dkimName := d.Selector + "._domainkey." + domain
	if txt, e := net.LookupTXT(dkimName); e != nil {
		problems = append(problems, "DKIM no encontrado en "+dkimName)
	} else if joined := strings.Join(txt, ""); len(d.DKIMPublic) >= 40 && !strings.Contains(joined, d.DKIMPublic[:40]) {
		problems = append(problems, "el DKIM publicado no coincide con el generado")
	}

	if txt, e := net.LookupTXT(domain); e != nil {
		problems = append(problems, "SPF no encontrado en "+domain)
	} else {
		ok := false
		for _, t := range txt {
			if strings.HasPrefix(t, "v=spf1") && strings.Contains(t, svc.publicIP) {
				ok = true
			}
		}
		if !ok {
			problems = append(problems, "el SPF de "+domain+" no incluye la IP "+svc.publicIP)
		}
	}

	if len(problems) > 0 {
		return false, problems, nil
	}
	if err := svc.st.MarkVerified(ctx, domain); err != nil {
		return false, nil, err
	}
	return true, nil, nil
}
