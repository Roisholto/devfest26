package agent

import (
	"context"
	"net/url"

	"devfest26/internal/domain"
	"devfest26/internal/httpx"
)

type PaymentActivities struct{ Client *httpx.Client }

func (a *PaymentActivities) SubmitTransfer(ctx context.Context, in domain.SubmitTransfer) (domain.Transfer, error) {
	var t domain.Transfer

	err := a.Client.Do(ctx, "POST", "/transfers", "", in, &t)

	return t, activityError(err)
}

func (a *PaymentActivities) ReadTransfer(ctx context.Context, key string) (domain.Transfer, error) {
	var t domain.Transfer

	err := a.Client.Do(ctx, "GET", "/transfers/"+url.PathEscape(key), "", nil, &t)

	return t, activityError(err)
}
