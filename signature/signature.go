package signature

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

const (
	tsHeader = "MessageBird-Request-Timestamp"
	sHeader  = "MessageBird-Signature"
)

// Window of acceptance for a request, if the time stamp is within this time, it will evaluated as valid
var ValidityWindow = 5 * time.Second

// StringToTime converts from Unicod Epoch enconded timestamps to time.Time Go objects
func stringToTime(s string) (time.Time, error) {
	sec, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return time.Time{}, err
	}
	return time.Unix(sec, 0), nil
}

// HMACSHA256 generates HMACS enconded hashes using the provided Key and SHA256
// encoding for the message
func hMACSHA256(message, key []byte) ([]byte, error) {
	mac := hmac.New(sha256.New, []byte(key))
	if _, err := mac.Write(message); err != nil {
		return nil, err
	}
	return mac.Sum(nil), nil
}

// Validator type represents a MessageBird signature validator
type Validator struct {
	SigningKey string         // Signing Key provided by MessageBird
	Period     *time.Duration // Period for a message to be accepted as real, set no nil to bypass the time validator
} // Five seconds by default

// NewValidator returns a signature validator object
func NewValidator(signingKey string) *Validator {
	return &Validator{
		SigningKey: signingKey,
		Period:     &ValidityWindow,
	}
}

// validTimestamp validates if the MessageBird-Request-Timestamp is a valid
// date and if the request is older than the validator Period.
func (v *Validator) validTimestamp(ts string) bool {
	t, err := stringToTime(ts)
	if err != nil {
		return false
	}
	if v.Period == nil {
		return true
	}

	diff := time.Now().Add(*v.Period / 2).Sub(t)
	return diff < *v.Period && diff > 0
}

// calculateSignature calculates the MessageBird-Signature using HMAC_SHA_256
// encoding and the timestamp, query params and body from the request:
// signature = HMAC_SHA_256(
//	TIMESTAMP + \n + QUERY_PARAMS + \n + SHA_256_SUM(BODY),
//	signing_key)
func (v *Validator) calculateSignature(ts, qp string, b []byte) ([]byte, error) {
	var m bytes.Buffer
	bh := sha256.Sum256(b)
	fmt.Fprintf(&m, "%s\n%s\n%s", ts, qp, bh[:])
	return hMACSHA256(m.Bytes(), []byte(v.SigningKey))
}

// validSignature takes the timestamp, query params and body from the request,
// calculates the expected signature and compares it to the one sent by MessageBird.
func (v *Validator) validSignature(ts, rqp string, b []byte, rs string) bool {
	uqp, err := url.Parse("?" + rqp)
	if err != nil {
		return false
	}
	es, err := v.calculateSignature(ts, uqp.Query().Encode(), b)
	if err != nil {
		return false
	}
	drs, err := base64.StdEncoding.DecodeString(rs)
	if err != nil {
		return false
	}
	return hmac.Equal(drs, es)
}

// ValidRequest is a method that takes care of the signature validation of
// incoming requests
// To use just pass the request:
// signature.Validate(request)
func (v *Validator) ValidRequest(r *http.Request) error {
	ts := r.Header.Get(tsHeader)
	rs := r.Header.Get(sHeader)
	if ts == "" || rs == "" {
		return fmt.Errorf("Unknown host: %s", r.Host)
	}
	b, _ := ioutil.ReadAll(r.Body)
	if v.validTimestamp(ts) == false || v.validSignature(ts, r.URL.RawQuery, b, rs) == false {
		return fmt.Errorf("Unknown host: %s", r.Host)
	}
	r.Body = ioutil.NopCloser(bytes.NewBuffer(b))
	return nil
}

// Validate is a handler wrapper that takes care of the signature validation of
// incoming requests and rejects them if invalid or pass them on to your handler
// otherwise.
// To use just wrap your handler with it:
// http.Handle("/path", signature.Validate(handleThing))
func (v *Validator) Validate(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := v.ValidRequest(r); err != nil {
			http.Error(w, "", http.StatusUnauthorized)
			return
		}
		h.ServeHTTP(w, r)
	})
}
