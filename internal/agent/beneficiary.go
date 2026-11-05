package agent

import (
	"context"

	"devfest26/internal/domain"
)

// AddBeneficiary is the participant exercise. The service and PostgreSQL
// boundary enforce the rules again; no confirmation is needed for this tool.
func (r *Router) AddBeneficiary(ctx context.Context, key string, in domain.AddBeneficiary) (map[string]any, error) {
	if err := in.Validate(); err != nil {
		return nil, err
	}

	var out domain.Beneficiary

	if err := r.Client.Do(ctx, "POST", "/beneficiaries", key, in, &out); err != nil {
		return nil, err
	}

	return toMap(out), nil
}
