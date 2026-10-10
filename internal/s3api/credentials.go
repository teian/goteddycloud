// SPDX-License-Identifier: GPL-2.0-only
package s3api

import (
	"bufio"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type Credentials struct{ AccessKey, SecretKey, Token string }

// LoadCredentials accepts standard AWS environment credentials or a named
// profile from the shared credentials file. It never logs secret values.
func LoadCredentials() (Credentials, error) {
	credentials := Credentials{os.Getenv("AWS_ACCESS_KEY_ID"), os.Getenv("AWS_SECRET_ACCESS_KEY"), os.Getenv("AWS_SESSION_TOKEN")}
	if credentials.AccessKey != "" || credentials.SecretKey != "" {
		if credentials.AccessKey == "" || credentials.SecretKey == "" {
			return Credentials{}, errors.New("incomplete S3 environment credentials")
		}
		return credentials, nil
	}
	name := os.Getenv("AWS_SHARED_CREDENTIALS_FILE")
	if name == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return Credentials{}, errors.New("S3 credentials unavailable")
		}
		name = filepath.Join(home, ".aws", "credentials")
	}
	file, err := os.Open(name)
	if err != nil {
		return Credentials{}, errors.New("S3 credentials unavailable: configure AWS_ACCESS_KEY_ID and AWS_SECRET_ACCESS_KEY or a shared credentials profile")
	}
	defer file.Close()
	profile := os.Getenv("AWS_PROFILE")
	if profile == "" {
		profile = "default"
	}
	scanner := bufio.NewScanner(io.LimitReader(file, 1<<20))
	section := ""
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.TrimSpace(line[1 : len(line)-1])
			continue
		}
		if section != profile || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		value = strings.TrimSpace(value)
		switch strings.TrimSpace(key) {
		case "aws_access_key_id":
			credentials.AccessKey = value
		case "aws_secret_access_key":
			credentials.SecretKey = value
		case "aws_session_token":
			credentials.Token = value
		}
	}
	if scanner.Err() != nil || credentials.AccessKey == "" || credentials.SecretKey == "" {
		return Credentials{}, errors.New("S3 credentials profile is missing or incomplete")
	}
	return credentials, nil
}
