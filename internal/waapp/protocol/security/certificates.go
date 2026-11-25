package security

import (
	"context"
	"crypto/rsa"
	"errors"
	"strconv"
)

type canonicalCertificate struct {
	PEM string `json:"pem"`
	ID  int    `json:"key_id"`
}

type canonicalCertificates struct {
	Encryption *canonicalCertificate `json:"encryption_pem"`
	Password   *canonicalCertificate `json:"password_pem"`
}

type canonicalKeys struct {
	encryption  *rsa.PublicKey
	password    *rsa.PublicKey
	passwordID  int
	passwordPEM string
}

func (c *Client) canonicalKeys(ctx context.Context) (*canonicalKeys, error) {
	result, err := query[struct {
		Certificates *canonicalCertificates `json:"xwa2_ent_get_certificates"`
	}](ctx, c, GetCertificates, variables(struct {
		Time    string `json:"request_time"`
		UseCase string `json:"use_case"`
	}{strconv.FormatInt(c.now().Unix(), 10), "CANONICAL"}))
	if err != nil {
		return nil, err
	}
	certs := result.Certificates
	if certs == nil || certs.Encryption == nil || certs.Password == nil {
		return nil, errors.New("canonical encryption certificates are missing")
	}
	encryption, err := parsePublicKey(certs.Encryption.PEM)
	if err != nil {
		return nil, err
	}
	password, err := parsePublicKey(certs.Password.PEM)
	if err != nil {
		return nil, err
	}
	return &canonicalKeys{encryption: encryption, password: password, passwordID: certs.Password.ID, passwordPEM: certs.Password.PEM}, nil
}
