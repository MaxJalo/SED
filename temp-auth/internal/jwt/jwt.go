package jwt

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

var (
	ErrInvalidToken = errors.New("invalid token")
	ErrExpiredToken = errors.New("token expired")
)

type Claims struct {
	Sub              string   `json:"sub"`
	JTI              string   `json:"jti"`
	OrganizationCode string   `json:"organization_code"`
	DepartmentCode   string   `json:"department_code"`
	Roles            []string `json:"roles"`
	IssuedAt         int64    `json:"iat"`
	ExpiresAt        int64    `json:"exp"`
}

func Sign(claims Claims, secret []byte) (string, error) {
	headerBytes, err := json.Marshal(map[string]string{
		"alg": "HS256",
		"typ": "JWT",
	})
	if err != nil {
		return "", err
	}
	payloadBytes, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}

	hEnc := base64.RawURLEncoding.EncodeToString(headerBytes)
	pEnc := base64.RawURLEncoding.EncodeToString(payloadBytes)
	signingInput := hEnc + "." + pEnc

	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(signingInput))
	sigEnc := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))

	return signingInput + "." + sigEnc, nil
}

func Verify(token string, secret []byte, now time.Time) (Claims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return Claims{}, ErrInvalidToken
	}

	signingInput := parts[0] + "." + parts[1]
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(signingInput))
	expectedSig := mac.Sum(nil)

	actualSig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return Claims{}, ErrInvalidToken
	}
	if !hmac.Equal(expectedSig, actualSig) {
		return Claims{}, ErrInvalidToken
	}

	payloadBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return Claims{}, ErrInvalidToken
	}
	var claims Claims
	if err := json.Unmarshal(payloadBytes, &claims); err != nil {
		return Claims{}, ErrInvalidToken
	}
	if claims.ExpiresAt <= now.Unix() {
		return Claims{}, ErrExpiredToken
	}
	return claims, nil
}
