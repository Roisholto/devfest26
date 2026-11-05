// Package domain contains the rules shared by tool and payment-service boundaries.
package domain

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
)

const DefaultLimitKobo int64 = 10_000_000

type Problem struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *Problem) Error() string { return e.Message }

func Invalid(message string) error {
	return &Problem{
		Code:    "invalid_argument",
		Message: message,
	}
}

func Conflict(message string) error {
	return &Problem{
		Code:    "conflict",
		Message: message,
	}
}

func NotFound(message string) error {
	return &Problem{
		Code:    "not_found",
		Message: message,
	}
}

type Beneficiary struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	BankCode      string `json:"bank_code"`
	AccountNumber string `json:"account_number"`
}
type AddBeneficiary struct {
	Name          string `json:"name"`
	BankCode      string `json:"bank_code"`
	AccountNumber string `json:"account_number"`
}

var accountRE = regexp.MustCompile(`^[0-9]{10}$`)
var amountRE = regexp.MustCompile(`^(0|[1-9][0-9]*)(\.[0-9]{1,2})?$`)

func (b AddBeneficiary) Validate() error {
	if strings.TrimSpace(b.Name) == "" || len(b.Name) > 80 || strings.IndexFunc(b.Name, unicode.IsControl) >= 0 {
		return Invalid("name must contain 1–80 characters without control characters")
	}

	if b.BankCode != "058" && b.BankCode != "044" && b.BankCode != "011" {
		return Invalid("supported demo bank codes: 058 (GTBank), 044 (Access), 011 (FirstBank)")
	}

	if !accountRE.MatchString(b.AccountNumber) {
		return Invalid("account_number must be a string of exactly 10 ASCII digits, including leading zeros")
	}

	return nil
}

func ParseAmount(s string, limit int64) (int64, error) {
	if !amountRE.MatchString(s) {
		return 0, Invalid("amount_naira must be a decimal string with at most two decimal places, without commas")
	}

	p := strings.SplitN(s, ".", 2)
	whole, err := strconv.ParseInt(p[0], 10, 64)

	if err != nil || whole > (1<<63-1-99)/100 {
		return 0, Invalid("amount is too large")
	}

	fraction := int64(0)

	if len(p) == 2 {
		f := p[1]

		if len(f) == 1 {
			f += "0"
		}

		fraction, _ = strconv.ParseInt(f, 10, 64)
	}

	n := whole*100 + fraction

	if err := ValidateAmount(n, "NGN", limit); err != nil {
		return 0, err
	}

	return n, nil
}

func ValidateAmount(n int64, currency string, limit int64) error {
	if currency != "NGN" {
		return Invalid("only NGN is supported")
	}

	if n <= 0 || n > limit {
		return Invalid(fmt.Sprintf("amount must be positive and at most ₦%.2f", float64(limit)/100))
	}

	return nil
}

func ValidateKey(s string) error {
	if len(s) < 1 || len(s) > 240 || strings.IndexFunc(s, unicode.IsControl) >= 0 {
		return Invalid("idempotency key must contain 1–240 characters without control characters")
	}

	return nil
}

func NewID() string {
	var b [16]byte

	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}

	return hex.EncodeToString(b[:])
}

func Fingerprint(v any) string {
	b, err := json.Marshal(v)

	if err != nil {
		panic(err)
	}

	h := sha256.Sum256(b)

	return hex.EncodeToString(h[:])
}

func Money(n int64) string {
	s := strconv.FormatInt(n/100, 10)

	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}

	return fmt.Sprintf("₦%s.%02d", s, n%100)
}

func BankName(code string) string {
	switch code {
	case "058":
		return "GTBank"
	case "044":
		return "Access"
	case "011":
		return "FirstBank"
	}

	return code
}

type Proposal struct {
	ID          string      `json:"id"`
	Beneficiary Beneficiary `json:"beneficiary"`
	AmountKobo  int64       `json:"amount_kobo"`
	Currency    string      `json:"currency"`
	PaymentKey  string      `json:"payment_key"`
	ExecuteAt   string      `json:"execute_at,omitempty"`
}
type SubmitTransfer struct {
	Key           string `json:"payment_key"`
	BeneficiaryID string `json:"beneficiary_id"`
	AmountKobo    int64  `json:"amount_kobo"`
	Currency      string `json:"currency"`
	Scenario      string `json:"scenario"`
}

func (p SubmitTransfer) Validate(limit int64) error {
	if err := ValidateKey(p.Key); err != nil {
		return err
	}

	if p.BeneficiaryID == "" {
		return Invalid("beneficiary_id is required")
	}

	if err := ValidateAmount(p.AmountKobo, p.Currency, limit); err != nil {
		return err
	}

	switch p.Scenario {
	case "success", "decline", "unavailable", "delayed", "lost-response":
		return nil
	}

	return Invalid("unknown simulation scenario")
}

type Transfer struct {
	Key           string    `json:"payment_key"`
	Reference     string    `json:"reference"`
	BeneficiaryID string    `json:"beneficiary_id"`
	AmountKobo    int64     `json:"amount_kobo"`
	Currency      string    `json:"currency"`
	Status        string    `json:"status"`
	Reason        string    `json:"reason,omitempty"`
	Scenario      string    `json:"scenario"`
	CreatedAt     time.Time `json:"created_at"`
}
