package execution

import "errors"

// ErrOrderUnknown means submission may have reached the venue. It is not a
// rejection: callers must retain the intent, slot and capital reservation.
var ErrOrderUnknown = errors.New("order outcome is unknown; reconciliation required")

// ErrIntentPending means this new request was NOT submitted because a prior
// intent with the same identity or an uncertain close is still outstanding.
var ErrIntentPending = errors.New("prior order intent is still pending")
