package slackclient

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"unicode"
	"unicode/utf8"
)

const AuthBundleLimit = 64 * 1024

var ErrInvalidAuthBundle = errors.New("invalid authentication bundle")

type authBundle struct {
	Format    string `json:"format"`
	Version   int    `json:"version"`
	Workspace struct {
		TeamID string `json:"team_id"`
		Name   string `json:"name"`
		Domain string `json:"domain,omitempty"`
	} `json:"workspace"`
	Credentials struct {
		AccessToken string `json:"access_token"`
		Cookie      string `json:"cookie"`
	} `json:"credentials"`
}

func DecodeAuthBundle(data []byte) (Token, error) {
	if len(data) > AuthBundleLimit || !utf8.Valid(data) {
		return Token{}, ErrInvalidAuthBundle
	}
	var bundle authBundle
	fields := authBundleFields{
		"format":  &bundle.Format,
		"version": &bundle.Version,
		"workspace": authBundleFields{
			"team_id": &bundle.Workspace.TeamID,
			"name":    &bundle.Workspace.Name,
			"domain":  &bundle.Workspace.Domain,
		},
		"credentials": authBundleFields{
			"access_token": &bundle.Credentials.AccessToken,
			"cookie":       &bundle.Credentials.Cookie,
		},
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	if err := decodeAuthObject(dec, fields); err != nil {
		return Token{}, ErrInvalidAuthBundle
	}
	if _, err := dec.Token(); err != io.EOF || bundle.Format != "slk-auth" || bundle.Version != 1 {
		return Token{}, ErrInvalidAuthBundle
	}
	token := Token{
		TeamID: bundle.Workspace.TeamID, TeamName: bundle.Workspace.Name, Domain: bundle.Workspace.Domain,
		AccessToken: bundle.Credentials.AccessToken, Cookie: bundle.Credentials.Cookie,
	}
	if !validAuthToken(token) {
		return Token{}, ErrInvalidAuthBundle
	}
	return token, nil
}

func EncodeAuthBundle(token Token) ([]byte, error) {
	if !validAuthToken(token) {
		return nil, ErrInvalidAuthBundle
	}
	var bundle authBundle
	bundle.Format, bundle.Version = "slk-auth", 1
	bundle.Workspace.TeamID, bundle.Workspace.Name, bundle.Workspace.Domain = token.TeamID, token.TeamName, token.Domain
	bundle.Credentials.AccessToken, bundle.Credentials.Cookie = token.AccessToken, token.Cookie
	data, err := json.MarshalIndent(bundle, "", "  ")
	if err != nil || len(data)+1 > AuthBundleLimit {
		return nil, ErrInvalidAuthBundle
	}
	return append(data, '\n'), nil
}

type authBundleFields map[string]any

func decodeAuthObject(dec *json.Decoder, fields authBundleFields) error {
	opening, err := dec.Token()
	if err != nil || opening != json.Delim('{') {
		return ErrInvalidAuthBundle
	}
	seen := make(map[string]bool, len(fields))
	for dec.More() {
		key, err := dec.Token()
		if err != nil {
			return ErrInvalidAuthBundle
		}
		name, ok := key.(string)
		field, known := fields[name]
		if !ok || !known || seen[name] {
			return ErrInvalidAuthBundle
		}
		seen[name] = true
		switch target := field.(type) {
		case *string:
			value, err := dec.Token()
			text, ok := value.(string)
			if err != nil || !ok {
				return ErrInvalidAuthBundle
			}
			*target = text
		case *int:
			if err := dec.Decode(target); err != nil {
				return ErrInvalidAuthBundle
			}
		case authBundleFields:
			if err := decodeAuthObject(dec, target); err != nil {
				return err
			}
		}
	}
	closing, err := dec.Token()
	if err != nil || closing != json.Delim('}') {
		return ErrInvalidAuthBundle
	}
	for name := range fields {
		if name != "domain" && !seen[name] {
			return ErrInvalidAuthBundle
		}
	}
	return nil
}

func validAuthTeamID(id string) bool {
	if len(id) < 2 || len(id) > 64 || id[0] != 'T' {
		return false
	}
	for _, c := range id[1:] {
		if (c < 'A' || c > 'Z') && (c < '0' || c > '9') {
			return false
		}
	}
	return true
}

func validAuthToken(token Token) bool {
	if !validAuthTeamID(token.TeamID) || !strings.HasPrefix(token.AccessToken, "xoxc-") || len(token.AccessToken) <= len("xoxc-") {
		return false
	}
	if !validAuthText(token.AccessToken, 16*1024, true) || !validAuthText(token.Cookie, 16*1024, true) || token.Cookie == "" {
		return false
	}
	if strings.Contains(token.Cookie, ",") || (&http.Cookie{Name: "d", Value: token.Cookie}).Valid() != nil {
		return false
	}
	domain := token.Domain
	if len(domain) >= len(".enterprise") && strings.EqualFold(domain[len(domain)-len(".enterprise"):], ".enterprise") {
		domain = domain[:len(domain)-len(".enterprise")]
	}
	if !validAuthText(token.TeamName, 1024, false) || len(domain) > 63 || (token.Domain != "" && domain == "") {
		return false
	}
	for i, c := range domain {
		if c == '-' && i != 0 && i != len(domain)-1 {
			continue
		}
		if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') {
			return false
		}
	}
	return true
}

func validAuthText(text string, limit int, secret bool) bool {
	if len(text) > limit || !utf8.ValidString(text) {
		return false
	}
	for _, c := range text {
		if unicode.IsControl(c) || unicode.Is(unicode.Cf, c) || (secret && unicode.IsSpace(c)) {
			return false
		}
	}
	return true
}
