package agentauth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
)

// Credential format: wsa_<clientid>_<secret>, where clientid is 8 lowercase
// hex characters and secret is 32 random bytes in unpadded base64url (43
// characters). The secret may itself contain '_' and '-', so parsing uses
// fixed offsets rather than splitting on '_'.
const (
	CredentialPrefix = "wsa_"
	clientIDLen      = 8
	secretBytes      = 32
	secretLen        = 43 // base64.RawURLEncoding.EncodedLen(32)
	credentialLen    = len(CredentialPrefix) + clientIDLen + 1 + secretLen
)

// FormatCredential builds the credential string shown once to the human.
func FormatCredential(clientID, secret string) string {
	return CredentialPrefix + clientID + "_" + secret
}

// ParseCredential splits a credential into client ID and secret.
func ParseCredential(credential string) (clientID, secret string, err error) {
	if len(credential) != credentialLen || !strings.HasPrefix(credential, CredentialPrefix) {
		return "", "", fmt.Errorf("not an agent credential")
	}
	clientID = credential[len(CredentialPrefix) : len(CredentialPrefix)+clientIDLen]
	if credential[len(CredentialPrefix)+clientIDLen] != '_' || !validClientID(clientID) {
		return "", "", fmt.Errorf("not an agent credential")
	}
	secret = credential[len(CredentialPrefix)+clientIDLen+1:]
	raw, decodeErr := base64.RawURLEncoding.Strict().DecodeString(secret)
	if decodeErr != nil || len(raw) != secretBytes {
		return "", "", fmt.Errorf("not an agent credential")
	}
	return clientID, secret, nil
}

// IsAgentCredential reports whether an Authorization header carries a wsa_
// bearer credential. Legacy routes may use it to reject agent credentials
// explicitly (they would also fail the legacy token comparison).
func IsAgentCredential(header string) bool {
	token, ok := strings.CutPrefix(header, "Bearer ")
	return ok && strings.HasPrefix(strings.TrimSpace(token), CredentialPrefix)
}

func validClientID(id string) bool {
	if len(id) != clientIDLen {
		return false
	}
	for _, r := range id {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}

func hashSecret(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

func newClientID() (string, error) {
	b := make([]byte, clientIDLen/2)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func newSecret() (string, error) {
	b := make([]byte, secretBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
