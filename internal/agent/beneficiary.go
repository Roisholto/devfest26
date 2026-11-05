package agent

import (
	"context"

	"devfest26/internal/domain"
)

// EXERCISE 1: add the add_beneficiary declaration here. The supplied dispatcher
// already routes that name to AddBeneficiary. See docs/beneficiary-exercise.md.
var beneficiaryTools = []Spec{}

// EXERCISE 2: validate the input, POST it using the stable key and return the
// saved beneficiary. The complete file is in docs/solutions/beneficiary.go.txt.
func (r *Router) AddBeneficiary(ctx context.Context, key string, in domain.AddBeneficiary) (map[string]any, error) {
	return nil, domain.Invalid("exercise: implement add_beneficiary")
}
