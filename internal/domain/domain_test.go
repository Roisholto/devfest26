package domain

import "testing"

func TestAccountNumbersAreExactASCIIStrings(t *testing.T) {
	for _, n := range []string{"0123454821", "0000000000", "1234567890"} {
		if err := (AddBeneficiary{Name: "Ayomide", BankCode: "058", AccountNumber: n}).Validate(); err != nil {
			t.Errorf("valid %q: %v", n, err)
		}
	}
	for _, n := range []string{"", "123456789", "12345678901", " 0123454821", "0123454821 ", "01234 4821", "01234a4821", "٠١٢٣٤٥٤٨٢١", "０１２３４５４８２１", "0123454821\n"} {
		if err := (AddBeneficiary{Name: "Ayomide", BankCode: "058", AccountNumber: n}).Validate(); err == nil {
			t.Errorf("accepted invalid %q", n)
		}
	}
}
func TestExactAmounts(t *testing.T) {
	for s, want := range map[string]int64{"0.01": 1, "10": 1000, "10000.5": 1000050, "100000": 10000000} {
		got, err := ParseAmount(s, DefaultLimitKobo)
		if err != nil || got != want {
			t.Errorf("%s: got %d, %v", s, got, err)
		}
	}
	for _, s := range []string{"0", "-1", "NaN", "1e3", "10,000", ".1", "01", "1.001", "100000.01", "9223372036854775807", "1 ", "１"} {
		if _, err := ParseAmount(s, DefaultLimitKobo); err == nil {
			t.Errorf("accepted %q", s)
		}
	}
}
