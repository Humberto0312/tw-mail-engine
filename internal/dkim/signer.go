package dkim

import (
	"bytes"
	"crypto"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"

	"github.com/emersion/go-msgauth/dkim"
)

// Signer firma mensajes con la llave privada de UN dominio.
// (Multi-dominio vendrá después leyendo llaves por tenant desde Mongo.)
type Signer struct {
	domain   string
	selector string
	key      crypto.Signer
}

// NewSigner parsea una llave privada RSA en PEM (PKCS1 o PKCS8).
func NewSigner(domain, selector, pemKey string) (*Signer, error) {
	block, _ := pem.Decode([]byte(pemKey))
	if block == nil {
		return nil, fmt.Errorf("DKIM: PEM inválido")
	}
	var key crypto.Signer
	if k, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		key = k
	} else if k2, err2 := x509.ParsePKCS8PrivateKey(block.Bytes); err2 == nil {
		rk, ok := k2.(*rsa.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("DKIM: la llave PKCS8 no es RSA")
		}
		key = rk
	} else {
		return nil, fmt.Errorf("DKIM: no se pudo parsear la llave privada (ni PKCS1 ni PKCS8)")
	}
	return &Signer{domain: domain, selector: selector, key: key}, nil
}

// Sign antepone la cabecera DKIM-Signature al mensaje.
func (s *Signer) Sign(raw []byte) ([]byte, error) {
	options := &dkim.SignOptions{
		Domain:                 s.domain,
		Selector:               s.selector,
		Signer:                 s.key,
		HeaderCanonicalization: dkim.CanonicalizationRelaxed,
		BodyCanonicalization:   dkim.CanonicalizationRelaxed,
		HeaderKeys:             []string{"From", "To", "Subject", "Date", "Message-ID", "MIME-Version", "Content-Type"},
	}
	var out bytes.Buffer
	if err := dkim.Sign(&out, bytes.NewReader(raw), options); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}
