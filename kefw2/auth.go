/*
Copyright © 2023-2026 Jens Hilligsøe

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in
all copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
THE SOFTWARE.
*/
package kefw2

// This file implements the HMAC_SHA256 authenticated channel exposed by the
// KEF W2 firmware on TLS port 4430. The protocol details were recovered by
// decompiling the official KEF Connect Android app (jadx):
//
//   - Credential derivation: k5/b.java:1103-1118
//   - HMAC signer:           C9/c.java
//   - User provisioning:     C9/s.java
//
// Username defaults to a hardcoded fallback UUID used by the app when no
// device id is available; the password is hex(MD5("yek_" + username + "_user")).
// Provisioning the user is idempotent ("already exists" is treated as success).
//
// TLS pinning against the embedded KEF-CA root is not implemented yet; we use
// InsecureSkipVerify on the 4430 endpoint until the cert is embedded.

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/md5"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"
)

func init() {
	if v := os.Getenv("KEFW2_AUTH_DEBUG"); v == "1" || v == "true" {
		authDebug = true
	}
}

const (
	// authPort is the TLS port serving the authenticated control API.
	authPort = 4430

	// authFallbackUsername mirrors the hardcoded fallback in
	// k5/b.java:1103-1118 used when the app has no device id.
	authFallbackUsername = "7f3c2a91-4d0b-4f65-9b3a-8e2f1c6d4a13"

	// authPathSetData is the endpoint used for both provisioning and
	// authenticated mutations.
	authPathSetData = "/api/setData"
)

var (
	authClientOnce sync.Once
	authClient     *http.Client

	// authDebug enables verbose stderr logging of the auth/provisioning flow.
	// Toggle via env KEFW2_AUTH_DEBUG=1 — see init() below.
	authDebug bool
)

func truncate(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "…"
}

// authHTTPClient returns a TLS client suitable for the 4430 endpoint.
//
// TODO: embed the KEF-CA root from the Android app (p.java) and pin against
// it instead of skipping verification.
func authHTTPClient() *http.Client {
	authClientOnce.Do(func() {
		authClient = &http.Client{
			Timeout: 5 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec
			},
		}
	})
	return authClient
}

// authCredentials returns the username/password pair used to talk to the
// authenticated API. Matches k5/b.java:1103-1118.
func authCredentials() (username, password string) {
	username = authFallbackUsername
	sum := md5.Sum([]byte("yek_" + username + "_user"))
	password = hex.EncodeToString(sum[:])
	return username, password
}

// provisionAuthUser performs an idempotent webserver:addUser POST so that
// subsequent signed requests are accepted. Mirrors C9/s.java.
func (s *KEFSpeaker) provisionAuthUser(ctx context.Context) error {
	username, password := authCredentials()
	body := map[string]any{
		"path": "webserver:addUser",
		"role": "activate",
		"value": map[string]string{
			"name":     username,
			"password": password,
			"title":    "",
		},
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal addUser body: %w", err)
	}

	url := fmt.Sprintf("https://%s:%d%s", s.IPAddress, authPort, authPathSetData)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return fmt.Errorf("build addUser request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := authHTTPClient().Do(req)
	if err != nil {
		return fmt.Errorf("addUser: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if authDebug {
		fmt.Printf("[auth] provision addUser status=%d resp=%s\n",
			resp.StatusCode, truncate(respBody, 300))
	}
	// 2xx → success. The firmware returns 500 with an "already exists" style
	// message when the user is present; treat that as success too.
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	if bytes.Contains(bytes.ToLower(respBody), []byte("exist")) {
		return nil
	}
	return fmt.Errorf("addUser: HTTP %d: %s", resp.StatusCode, string(respBody))
}

// signAuthHeader builds the HMAC_SHA256 Authorization header value.
// Mirrors C9/c.java. The canonical string uses the FULL request URL
// (scheme://host:port/path), not just the path — see Jd/s.java:36,208-210
// where field `i` is the full URL and `toString()` returns it verbatim,
// and C9/c.java:144 appends `sVar.i` directly. For POST the raw request
// body is appended (NOT a hash of it).
func signAuthHeader(username, password, fullURL string, body []byte, isPOST bool) (string, error) {
	nonceRaw := make([]byte, 6)
	if _, err := rand.Read(nonceRaw); err != nil {
		return "", fmt.Errorf("nonce: %w", err)
	}
	nonce := base64.StdEncoding.EncodeToString(nonceRaw)
	// Double base64-encode: encode the UTF-8 bytes of the first encoding.
	nonceB64 := base64.StdEncoding.EncodeToString([]byte(nonce))
	usernameB64 := base64.StdEncoding.EncodeToString([]byte(username))
	timestamp := strconv.FormatInt(time.Now().UnixMilli(), 10)

	// derivedKey = SHA-256(nonce_utf8 + password_utf8), raw 32 bytes.
	h := sha256.New()
	h.Write([]byte(nonce))
	h.Write([]byte(password))
	derivedKey := h.Sum(nil)

	// NOTE: canonical uses the DOUBLE-base64 nonce (nonceB64), not the raw
	// single-encoded nonce. See C9/c.java:140 (strEncodeToString2).
	canonical := username + "." + nonceB64 + "." + timestamp + "." + fullURL
	if isPOST {
		canonical += "." + string(body)
	}

	mac := hmac.New(sha256.New, derivedKey)
	mac.Write([]byte(canonical))
	signature := base64.StdEncoding.EncodeToString(mac.Sum(nil))

	return "HMAC_SHA256 " + usernameB64 + "." + nonceB64 + "." + timestamp + "." + signature, nil
}

// doAuthenticatedPOST issues a signed POST to the 4430 endpoint, provisioning
// the auth user on first 401.
func (s *KEFSpeaker) doAuthenticatedPOST(ctx context.Context, path string, body []byte) (*http.Response, []byte, error) {
	username, password := authCredentials()

	url := fmt.Sprintf("https://%s:%d%s", s.IPAddress, authPort, path)

	send := func() (*http.Response, []byte, error) {
		auth, err := signAuthHeader(username, password, url, body, true)
		if err != nil {
			return nil, nil, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
		if err != nil {
			return nil, nil, fmt.Errorf("build request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", auth)

		resp, err := authHTTPClient().Do(req)
		if err != nil {
			return nil, nil, err
		}
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if authDebug {
			fmt.Printf("[auth] POST %s status=%d canonical-len=%d body-len=%d resp=%s\n",
				url, resp.StatusCode, len(auth), len(body), truncate(raw, 200))
		}
		return resp, raw, nil
	}

	resp, raw, err := send()
	if err != nil {
		return nil, nil, err
	}
	if resp.StatusCode != http.StatusUnauthorized {
		return resp, raw, nil
	}

	// Likely first-run: user not yet provisioned. Provision and retry once.
	if err := s.provisionAuthUser(ctx); err != nil {
		return nil, nil, fmt.Errorf("provision after 401: %w", err)
	}
	return send()
}
