package agent

import (
	"context"

	"devfest26/internal/domain"
)

// AddBeneficiary saves details supplied by an add_beneficiary tool call.
// EXERCISE 2: validate the input, POST it using the stable key and return the
// saved beneficiary. The complete file is in docs/solutions/beneficiary.go.txt.
func (r *ToolDispatcher) AddBeneficiary(ctx context.Context, key string, in domain.AddBeneficiaryRequest) (map[string]any, error) {
	return nil, domain.Invalid("exercise: implement add_beneficiary")
}
